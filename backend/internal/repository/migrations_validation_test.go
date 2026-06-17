package repository

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
)

// TestEmbeddedMigrationsPassExecutionModeValidation 校验所有随二进制 embed 的迁移文件
// 都能通过 runner 的执行模式校验（validateMigrationExecutionMode）。
//
// 这等价于服务启动时 ApplyMigrations 对每个迁移做的前置校验，但不连数据库。
// 目的：把"迁移文件格式错误"（如普通迁移里出现 CONCURRENTLY 字样、_notx 文件混入
// 事务控制语句等）在本地 go test 阶段拦下，而不是等部署到生产触发 health-check
// 失败 + auto-rollback 才发现。
//
// 历史教训：103 迁移曾因注释中含 "CONCURRENTLY" 字样触发 runner 的全文本匹配校验，
// 在生产启动时被拒导致 rollback。此测试守护该类回归。
func TestEmbeddedMigrationsPassExecutionModeValidation(t *testing.T) {
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatalf("glob embedded migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no embedded migrations found — embed glob likely broken")
	}

	for _, name := range files {
		b, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		content := strings.TrimSpace(string(b))
		if content == "" {
			continue
		}
		if _, err := validateMigrationExecutionMode(name, content); err != nil {
			t.Errorf("%s failed execution-mode validation: %v", name, err)
		}
	}
}

// TestValidateMigrationExecutionMode_RejectsConcurrentlyInPlainMigration 是上面测试的
// negative 对照：确认校验函数确实会拒绝"普通 .sql 迁移中出现 CONCURRENTLY"，
// 否则上面的 positive 测试可能因校验形同虚设而给出假安全感。
func TestValidateMigrationExecutionMode_RejectsConcurrentlyInPlainMigration(t *testing.T) {
	// 普通迁移文件名（无 _notx 后缀）含 CONCURRENTLY → 必须报错。
	if _, err := validateMigrationExecutionMode("999_bad.sql", "CREATE INDEX CONCURRENTLY foo ON bar(x);"); err == nil {
		t.Fatal("expected error for CONCURRENTLY in a non-_notx migration, got nil")
	}

	// _notx 迁移里混入事务控制语句 → 必须报错。
	if _, err := validateMigrationExecutionMode("999_bad_notx.sql", "BEGIN; CREATE INDEX CONCURRENTLY foo ON bar(x); COMMIT;"); err == nil {
		t.Fatal("expected error for transaction control in _notx migration, got nil")
	}

	// 合法的普通迁移 → 不应报错。
	if _, err := validateMigrationExecutionMode("999_ok.sql", "ALTER TABLE groups DROP COLUMN IF EXISTS is_exclusive;"); err != nil {
		t.Fatalf("valid plain migration rejected: %v", err)
	}
}
