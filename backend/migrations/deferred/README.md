# 延迟迁移（contract 阶段）—— 不会被自动执行

本目录中的文件**不在** `migrations/*.sql` 的 embed/执行范围内（`//go:embed *.sql` 与
`fs.Glob(fsys, "*.sql")` 均不递归子目录），因此服务启动时**不会**自动应用它们。

这是 expand-contract 迁移模式的 contract 阶段暂存区：先在 `migrations/` 用 expand 迁移
（只加不删）上线，确认新版本稳定、不再需要回退旧二进制后，再启用对应的 contract 迁移。

## 如何启用一个延迟迁移

1. 确认前置 expand 迁移已在所有环境稳定运行，且无回退旧版本的需求。
2. 将文件移动到 `migrations/` 并改回 `.sql` 后缀，按当时的最新编号重命名（保持递增）：
   ```
   git mv backend/migrations/deferred/103_drop_groups_is_exclusive.sql.deferred \
          backend/migrations/<下一个编号>_drop_groups_is_exclusive.sql
   ```
3. 提交并正常发版——下次服务启动会自动执行该迁移。

## 当前待启用清单

（空）

## 已启用历史

- `103_drop_groups_is_exclusive.sql` —— 已于 contract 阶段移回 `migrations/` 并执行，
  删除了旧布尔列 `groups.is_exclusive`。
