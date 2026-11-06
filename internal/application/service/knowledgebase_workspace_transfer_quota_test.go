package service

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestWorkspaceTransferRechecksTargetQuotaAtCommit(t *testing.T) {
	for _, tc := range []struct {
		name        string
		used, quota int64
		blocked     bool
	}{
		{"quota lowered", 0, 50, true},
		{"usage increased", 95, 100, true},
		{"exact remaining space", 20, 120, false},
		{"unlimited", 95, 0, false},
		{"overflow", math.MaxInt64 - 10, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, db, baseDir, kb := newWorkspaceTransferIntegrationFixture(t)
			// The fixture's tenant repository deliberately returns the old preflight
			// snapshot. Only the transaction sees this quota/usage change.
			require.NoError(t, db.Model(&types.Tenant{}).Where("id = ?", 10001).
				Updates(map[string]any{"storage_used": tc.used, "storage_quota": tc.quota}).Error)
			_, err := svc.TransferKnowledgeBaseWorkspace(transferTestCtx("creator", types.TenantRoleContributor, 10000), kb.ID, "new")
			var target, source types.Tenant
			require.NoError(t, db.First(&target, 10001).Error)
			require.NoError(t, db.First(&source, 10000).Error)
			var stored types.KnowledgeBase
			require.NoError(t, db.First(&stored, "id = ?", kb.ID).Error)
			if tc.blocked {
				var blocked *KBWorkspaceTransferBlockedError
				require.ErrorAs(t, err, &blocked)
				require.Len(t, blocked.Blockers, 1)
				assert.Equal(t, "target_quota_exceeded", blocked.Blockers[0].Code)
				assert.Equal(t, tc.used, target.StorageUsed)
				assert.Equal(t, int64(100), source.StorageUsed)
				assert.Equal(t, uint64(10000), stored.TenantID)
				assert.FileExists(t, filepath.Join(baseDir, "10000/k-1/report.pdf"))
				assert.NoFileExists(t, filepath.Join(baseDir, "10001/k-1/report.pdf"))
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.used+100, target.StorageUsed)
				assert.Zero(t, source.StorageUsed)
				assert.Equal(t, uint64(10001), stored.TenantID)
			}
		})
	}
}

func TestWorkspaceTransferQuotaLocksInOrderAndUsesAtomicUpdates(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "forward", true: "reverse"}[reverse], func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer sqlDB.Close()
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
			require.NoError(t, err)
			oldTenant, newTenant := uint64(1), uint64(2)
			if reverse {
				oldTenant, newTenant = newTenant, oldTenant
			}
			mock.ExpectBegin()
			for _, id := range []uint64{1, 2} {
				mock.ExpectQuery(`SELECT .*FROM "tenants".*FOR UPDATE`).WithArgs(id, 1).
					WillReturnRows(sqlmock.NewRows([]string{"id", "storage_used", "storage_quota"}).AddRow(id, 100, 1000))
			}
			mock.ExpectExec(`UPDATE "tenants" SET "storage_used"=storage_used \+`).
				WithArgs(int64(10), newTenant, int64(math.MaxInt64-10), int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`UPDATE "tenants" SET "storage_used"=CASE WHEN storage_used <`).
				WithArgs(int64(10), int64(10), oldTenant).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return adjustKBWorkspaceTransferQuota(tx, oldTenant, newTenant, 10) }))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
