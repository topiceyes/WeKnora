package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

const (
	retrievalRepeatWarnRounds = 2
	retrievalRepeatStopRounds = 3
	maxRetrievalObservations  = 128
)

const retrievalRepeatNotice = `<runtime_event>{"source":"runtime","type":"repeated_retrieval","message":"Recent knowledge retrieval rounds repeated previously executed calls and returned identical results. Do not repeat those calls. Use the evidence already obtained to answer, or make a different targeted retrieval if evidence is still missing. For missing Wiki pages, search for an actual page slug instead of guessing or retrying the same slug. State evidence gaps explicitly."}</runtime_event>`

const retrievalStopNotice = `<runtime_event>{"source":"runtime","type":"retrieval_stopped","message":"Knowledge retrieval continued to repeat identical calls and results after a warning. Retrieval has now stopped. Give a substantive answer using only the evidence actually obtained. If the evidence is insufficient, explain what is missing. Do not end with a promise to search, a process update, or a claim that missing facts were verified."}</runtime_event>`

// retrievalGuard watches completed local KB reads, never writes, MCP calls,
// shell commands or status-polling tools. Every call still executes, so a
// changed result can clear the stall before it stops a turn. Only hashes are
// retained, bounded to this active execution; source text is not cached here.
type retrievalGuard struct {
	seen           map[[sha256.Size]byte]struct{}
	order          [maxRetrievalObservations][sha256.Size]byte
	next           int
	repeatedRounds int
}

func guardedRetrieval(name string) bool {
	switch name {
	case agenttools.ToolSearchKnowledge, agenttools.ToolReadDocument,
		agenttools.ToolListDocuments, agenttools.ToolQueryKnowledgeGraph,
		agenttools.ToolWikiSearch, agenttools.ToolWikiReadPage:
		return true
	default:
		return false
	}
}

func retrievalFingerprint(call types.ToolCall) ([sha256.Size]byte, bool) {
	var fingerprint [sha256.Size]byte
	if !guardedRetrieval(call.Name) || call.Target != nil || call.Result == nil || call.Result.Suspend != nil {
		return fingerprint, false
	}
	h := sha256.New()
	// JSON canonicalizes map-key order. Call IDs, timings, reflection and
	// provider metadata change on every execution and are not evidence.
	err := json.NewEncoder(h).Encode(struct {
		Name              string
		Args              map[string]interface{}
		Result            *types.ToolResult
		ImageDescriptions []string
	}{call.Name, call.Args, call.Result, call.Result.ImageDescriptions})
	if err != nil {
		return fingerprint, false
	}
	copy(fingerprint[:], h.Sum(nil))
	return fingerprint, true
}

// observe counts only rounds wholly made of previously observed calls AND
// results. New queries, pagination or changed results count as progress. A
// mixed/non-retrieval round invalidates all prior observations: its side
// effects or external work may justify reading the same source again.
func (g *retrievalGuard) observe(step types.AgentStep) (warn bool) {
	if len(step.ToolCalls) == 0 || len(step.UserMessagesBefore) > 0 {
		*g = retrievalGuard{}
	}
	keys := make([][sha256.Size]byte, 0, len(step.ToolCalls))
	for _, call := range step.ToolCalls {
		key, ok := retrievalFingerprint(call)
		if !ok {
			*g = retrievalGuard{}
			return false
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return false
	}
	repeated := true
	for _, key := range keys {
		if _, ok := g.seen[key]; !ok {
			repeated = false
		}
	}
	if repeated {
		g.repeatedRounds++
	} else {
		g.repeatedRounds = 0
	}
	if g.seen == nil {
		g.seen = make(map[[sha256.Size]byte]struct{})
	}
	for _, key := range keys {
		if _, ok := g.seen[key]; ok {
			continue
		}
		if len(g.seen) == maxRetrievalObservations {
			delete(g.seen, g.order[g.next])
		}
		g.order[g.next] = key
		g.next = (g.next + 1) % maxRetrievalObservations
		g.seen[key] = struct{}{}
	}
	return g.repeatedRounds == retrievalRepeatWarnRounds
}

// Called at the next round boundary, after steering and collaboration input
// were drained. New instructions must be able to clear a pending stop.
func (e *AgentEngine) finishRepeatedRetrieval(
	ctx context.Context, state *types.AgentState, query, sessionID string, messages []chat.Message,
) error {
	common.PipelineWarn(ctx, "Agent", "repeated_retrieval_stop", map[string]interface{}{
		"iteration": state.CurrentRound, "repeated_rounds": retrievalRepeatStopRounds,
	})
	if e.collaborationOwnsFailure(ctx) {
		return fmt.Errorf("knowledge retrieval stopped after repeated calls without new evidence")
	}
	if err := e.streamFinalAnswerWithNotice(ctx, query, state, sessionID, messages, retrievalStopNotice); err != nil {
		return fmt.Errorf("synthesize answer after repeated retrieval: %w", err)
	}
	if strings.TrimSpace(state.FinalAnswer) == "" {
		e.finishStalledTurn(ctx, state, sessionID, &types.ChatResponse{
			Content: "Knowledge retrieval kept returning the same results, and I could not complete an answer from the available evidence. Please narrow the question or check the source documents.",
		}, false)
	}
	state.IsComplete = true
	return nil
}
