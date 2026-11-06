package container

import (
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/kbmutation"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"go.uber.org/dig"
	"gorm.io/gorm"
)

func newKnowledgeMutationCoordinator() *kbmutation.Coordinator {
	baseDir := strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR"))
	if baseDir == "" {
		baseDir = "/data/files"
	}
	return &kbmutation.Coordinator{BaseDir: baseDir}
}

type knowledgeMutationParams struct {
	dig.In
	Coordinator   *kbmutation.Coordinator
	DB            *gorm.DB
	Knowledge     interfaces.KnowledgeService
	KB            interfaces.KnowledgeBaseService
	Chunks        interfaces.ChunkService
	Tags          interfaces.KnowledgeTagService
	Pages         interfaces.WikiPageService
	Sources       interfaces.DataSourceService
	Lint          *service.WikiLintService
	Housekeeping  *service.HousekeepingService
	Resources     interfaces.ResourceCatalog
	Shares        interfaces.KBShareService
	Organizations interfaces.OrganizationService
	Scheduler     *datasource.Scheduler
	Extract       interfaces.TaskHandler `name:"chunkExtractor"`
	TableSummary  interfaces.TaskHandler `name:"dataTableSummary"`
	Images        interfaces.TaskHandler `name:"imageMultimodal"`
	PostProcess   interfaces.TaskHandler `name:"knowledgePostProcess"`
	AutoTag       interfaces.TaskHandler `name:"knowledgeAutoTag"`
	Profile       interfaces.TaskHandler `name:"knowledgeBaseProfile"`
	Wiki          interfaces.TaskHandler `name:"wikiIngest"`
}

// Configure every writer before cron, task consumers or HTTP can start.
func wireKnowledgeMutationCoordination(p knowledgeMutationParams) error {
	if err := service.ConfigureKBMutationCoordination(p.Coordinator, p.DB,
		p.Knowledge, p.KB, p.Chunks, p.Tags, p.Pages, p.Sources, p.Lint,
		p.Housekeeping, p.Resources, p.Shares, p.Organizations, p.Extract, p.TableSummary, p.Images,
		p.PostProcess, p.AutoTag, p.Profile, p.Wiki); err != nil {
		return err
	}
	p.Scheduler.SetMutationCoordinator(p.Coordinator)
	return nil
}
