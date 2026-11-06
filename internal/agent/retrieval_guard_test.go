package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func retrievalStep(query, output string) types.AgentStep {
	return types.AgentStep{ToolCalls: []types.ToolCall{{
		Name: agenttools.ToolWikiSearch, Args: map[string]interface{}{"query": query},
		Result: &types.ToolResult{Success: true, Output: output},
	}}}
}

func TestRetrievalGuardTracksCallsAndEvidenceAcrossAlternatingRounds(t *testing.T) {
	var g retrievalGuard
	a, b := retrievalStep("topic A", "page A"), retrievalStep("topic B", "page B")
	assert.False(t, g.observe(a))
	assert.False(t, g.observe(b))
	assert.False(t, g.observe(a))
	assert.True(t, g.observe(b), "alternating known calls should warn too")
	assert.False(t, g.observe(a), "the warning fires only once")
	assert.Equal(t, retrievalRepeatStopRounds, g.repeatedRounds)
}

func TestRetrievalGuardProgressAndInvalidation(t *testing.T) {
	for _, name := range []string{"new query", "pagination", "changed output", "changed data", "changed error", "changed image", "write", "mixed", "proxy", "missing result", "plain round", "new instruction"} {
		t.Run(name, func(t *testing.T) {
			var g retrievalGuard
			step := retrievalStep("topic", "page A")
			g.observe(step)
			g.observe(step)
			require.True(t, g.observe(step))
			switch name {
			case "new query":
				step.ToolCalls[0].Args["query"] = "more specific topic"
			case "pagination":
				step.ToolCalls[0].Args["offset"] = 10
			case "changed output":
				step.ToolCalls[0].Result.Output = "page B"
			case "changed data":
				step.ToolCalls[0].Result.Data = map[string]interface{}{"source": "new source"}
			case "changed error":
				step.ToolCalls[0].Result.Success = false
				step.ToolCalls[0].Result.Error = "not found"
			case "changed image":
				step.ToolCalls[0].Result.Images = []string{"new image"}
			case "write":
				step.ToolCalls[0].Name = agenttools.ToolWikiWritePage
			case "mixed":
				step.ToolCalls = append(step.ToolCalls, types.ToolCall{Name: agenttools.ToolCallMCPTool})
			case "proxy":
				step.ToolCalls[0].Target = &types.ToolCallTarget{Name: "external"}
			case "missing result":
				step.ToolCalls[0].Result = nil
			case "plain round":
				step.ToolCalls = nil
			case "new instruction":
				step.UserMessagesBefore = []string{"new-user-message"}
			}
			assert.False(t, g.observe(step))
			assert.Zero(t, g.repeatedRounds)
		})
	}
}

func TestRetrievalGuardDoesNotCountDuplicatesWithinOneBatchAsRepeatedRounds(t *testing.T) {
	var g retrievalGuard
	step := retrievalStep("topic", "page")
	step.ToolCalls = append(step.ToolCalls, step.ToolCalls[0])
	g.observe(step)
	assert.Zero(t, g.repeatedRounds)
	g.observe(step)
	assert.Equal(t, 1, g.repeatedRounds)
}

func TestRetrievalGuardIgnoresCallIDsAndBoundsMemory(t *testing.T) {
	var g retrievalGuard
	for i := 0; i < maxRetrievalObservations+50; i++ {
		g.observe(retrievalStep(fmt.Sprintf("topic-%d", i), "page"))
	}
	assert.Len(t, g.seen, maxRetrievalObservations)
	step := retrievalStep(fmt.Sprintf("topic-%d", maxRetrievalObservations+49), "page")
	step.ToolCalls[0].ID = "new-call-id"
	step.ToolCalls[0].Duration = 999
	step.ToolCalls[0].Reflection = "thinking changed"
	g.observe(step)
	assert.Equal(t, 1, g.repeatedRounds)
	g.observe(retrievalStep("topic-0", "page"))
	assert.Zero(t, g.repeatedRounds, "an evicted observation is read as new evidence")
}

func retrievalToolResponse(id, name, args string) mockResponse {
	return mockResponse{chunks: []types.StreamResponse{{
		Content: "我再检索一下。", ResponseType: types.ResponseTypeAnswer,
		ToolCalls: []types.LLMToolCall{{ID: id, Type: "function", Function: types.FunctionCall{Name: name, Arguments: args}}},
		Done:      true, FinishReason: "tool_calls",
	}}}
}

func retrievalFinalResponse(content string) mockResponse {
	return mockResponse{chunks: []types.StreamResponse{{Content: content, ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "stop"}}}
}

// Proves an unlimited agent stops tool-bearing loops, gives the model a
// recovery chance, and synthesizes an actual answer with tools disabled.
func TestExecuteLoopRepeatedRetrievalSynthesizesFromExistingEvidence(t *testing.T) {
	for _, failedRead := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed_read_%v", failedRead), func(t *testing.T) {
			model := &mockChat{}
			for i := 0; i <= retrievalRepeatStopRounds; i++ {
				model.responses = append(model.responses, retrievalToolResponse(fmt.Sprintf("call-%d", i), agenttools.ToolWikiReadPage, `{"slugs":["missing-page"]}`))
			}
			const final = "根据已获得的资料，可以确认 A；B 缺少证据，无法确认。"
			model.responses = append(model.responses, retrievalFinalResponse(final))
			engine := newTestEngine(t, model, withMaxIterations(types.UnlimitedMaxIterations))
			tool := &retrievalTestTool{BaseTool: agenttools.NewBaseTool(agenttools.ToolWikiReadPage, "test", json.RawMessage(`{"type":"object"}`))}
			if failedRead {
				tool.result = func(int) *types.ToolResult { return &types.ToolResult{Success: false, Error: "Wiki page not found"} }
			}
			engine.toolRegistry = agenttools.NewToolRegistry()
			engine.toolRegistry.RegisterTool(tool)
			var completed int
			engine.eventBus.On(event.EventAgentComplete, func(context.Context, event.Event) error { completed++; return nil })
			recorder := &truncationRecorder{}
			recorder.attach(engine.eventBus)
			state := &types.AgentState{}
			_, err := engine.executeLoop(context.Background(), state, "question", emptyMessages(), emptyTools(), "s", "m")
			require.NoError(t, err)
			assert.True(t, state.IsComplete)
			assert.Equal(t, final, state.FinalAnswer)
			assert.Equal(t, 1+retrievalRepeatStopRounds, tool.calls, "all reads execute to check for fresh results")
			assert.Len(t, state.RoundSteps, tool.calls)
			assert.Equal(t, 1, completed)
			assert.True(t, recorder.doneEvent(t).Done)
			require.Len(t, model.opts, tool.calls+1)
			assert.Contains(t, model.opts[retrievalRepeatWarnRounds+1].RuntimeNotice, "repeated_retrieval")
			assert.Equal(t, "none", model.opts[len(model.opts)-1].ToolChoice)
			assert.Empty(t, model.opts[len(model.opts)-1].Tools)
			assert.Contains(t, model.opts[len(model.opts)-1].RuntimeNotice, "retrieval_stopped")
			var results int
			for _, msg := range model.calls[len(model.calls)-1] {
				if msg.Role == "tool" {
					results++
				}
			}
			assert.Equal(t, tool.calls, results, "synthesis retains evidence in tool roles")
		})
	}
}

type retrievalTestTool struct {
	agenttools.BaseTool
	calls  int
	result func(int) *types.ToolResult
	after  func(int)
}

func (t *retrievalTestTool) Execute(context.Context, json.RawMessage) (*types.ToolResult, error) {
	t.calls++
	if t.after != nil {
		t.after(t.calls)
	}
	if t.result != nil {
		return t.result(t.calls), nil
	}
	return &types.ToolResult{Success: true, Output: "unchanged evidence"}, nil
}

func TestExecuteLoopRetrievalGuardAllowsRecoveryAndUserSteering(t *testing.T) {
	for _, mode := range []string{"changed result", "different query", "user correction", "write"} {
		t.Run(mode, func(t *testing.T) {
			model := &mockChat{}
			for i := 0; i < 7; i++ {
				args := `{"query":"topic"}`
				name := agenttools.ToolWikiSearch
				if mode == "different query" {
					args = fmt.Sprintf(`{"query":"topic-%d"}`, i)
				}
				if mode == "write" && i == 3 {
					name = agenttools.ToolWikiWritePage
				}
				model.responses = append(model.responses, retrievalToolResponse(fmt.Sprintf("call-%d", i), name, args))
			}
			model.responses = append(model.responses, retrievalFinalResponse("finished naturally"))
			engine := newTestEngine(t, model, withMaxIterations(types.UnlimitedMaxIterations))
			tool := &retrievalTestTool{BaseTool: agenttools.NewBaseTool(agenttools.ToolWikiSearch, "test", json.RawMessage(`{"type":"object"}`))}
			sink := &fakeSteerSink{}
			engine.SetSteerSink(sink)
			if mode == "user correction" {
				tool.after = func(calls int) {
					if calls == 4 {
						sink.queued = append(sink.queued, steerEntry("new", "请重新核实"))
					}
				}
			}
			if mode == "changed result" {
				tool.result = func(calls int) *types.ToolResult {
					return &types.ToolResult{Success: true, Output: fmt.Sprintf("evidence-%d", (calls-1)/3)}
				}
			}
			engine.toolRegistry = agenttools.NewToolRegistry()
			engine.toolRegistry.RegisterTool(tool)
			engine.toolRegistry.RegisterTool(newCountingTool(agenttools.ToolWikiWritePage))
			state := &types.AgentState{}
			_, err := engine.executeLoop(context.Background(), state, "question", emptyMessages(), emptyTools(), "s", "m")
			require.NoError(t, err)
			assert.Equal(t, "finished naturally", state.FinalAnswer)
			assert.Len(t, state.RoundSteps, 8)
			for _, opts := range model.opts {
				assert.NotContains(t, opts.RuntimeNotice, "retrieval_stopped")
			}
			if mode == "user correction" {
				require.Len(t, sink.persisted, 1)
				assert.Contains(t, strings.TrimSpace(model.calls[4][len(model.calls[4])-1].Content), "请重新核实")
			}
		})
	}
}

func TestRepeatedRetrievalCannotCompleteUnreviewedDelegatedWork(t *testing.T) {
	r, binding := engineCollaboration(t)
	_, err := r.Spawn(t.Context(), binding.Scope, "spawn", "research", "template", "{}", "task")
	require.NoError(t, err)
	engine := newTestEngine(t, &mockChat{})
	engine.SetCollaboration(binding)
	state := &types.AgentState{}
	err = engine.finishRepeatedRetrieval(t.Context(), state, "task", "s", emptyMessages())
	require.ErrorContains(t, err, "without new evidence")
	assert.False(t, state.IsComplete)
	assert.Empty(t, state.FinalAnswer)
}

func TestRetrievalGuardNewReadAfterMutationIsNotStale(t *testing.T) {
	var g retrievalGuard
	step := retrievalStep("topic", "page")
	g.observe(step)
	g.observe(step)
	g.observe(step)
	write := types.AgentStep{ToolCalls: []types.ToolCall{{Name: agenttools.ToolWikiWritePage}}}
	g.observe(write)
	g.observe(step)
	assert.Zero(t, g.repeatedRounds)
}

func TestRetrievalGuardCanonicalizesArguments(t *testing.T) {
	var g retrievalGuard
	step := retrievalStep("topic", "page")
	step.ToolCalls[0].Args["limit"] = 10
	g.observe(step)
	step.ToolCalls[0].Args = map[string]interface{}{"limit": 10, "query": "topic"}
	g.observe(step)
	assert.Equal(t, 1, g.repeatedRounds)
}

func TestRepeatedRetrievalSynthesisFailureIsNotSuccess(t *testing.T) {
	for _, mode := range []string{"provider failure", "provider requests more tools"} {
		t.Run(mode, func(t *testing.T) {
			model := &mockChat{}
			if mode == "provider requests more tools" {
				model.responses = []mockResponse{retrievalToolResponse("ignored", agenttools.ToolWikiSearch, `{"query":"again"}`)}
			}
			engine := newTestEngine(t, model)
			state := &types.AgentState{}
			err := engine.finishRepeatedRetrieval(t.Context(), state, "task", "s", emptyMessages())
			require.Error(t, err)
			assert.False(t, state.IsComplete)
			assert.Empty(t, state.FinalAnswer, "a preamble cannot masquerade as the completed answer")
		})
	}
}

func TestRepeatedRetrievalEmptySynthesisExplainsFailure(t *testing.T) {
	model := &mockChat{responses: []mockResponse{retrievalFinalResponse("")}}
	engine := newTestEngine(t, model)
	recorder := &truncationRecorder{}
	recorder.attach(engine.eventBus)
	state := &types.AgentState{}
	require.NoError(t, engine.finishRepeatedRetrieval(t.Context(), state, "task", "s", emptyMessages()))
	assert.True(t, state.IsComplete)
	assert.Contains(t, state.FinalAnswer, "could not complete an answer")
	var emitted strings.Builder
	for _, evt := range recorder.snapshot() {
		emitted.WriteString(evt.Content)
	}
	assert.Equal(t, state.FinalAnswer, emitted.String())
}
