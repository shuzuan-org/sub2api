-- 分组可见性 expand-contract 模式的【contract 阶段】：删除旧布尔列 is_exclusive。
--
-- 前置：102 已执行（visibility 列存在并完成回填），新版本二进制已稳定运行。
-- 不可逆：执行后，读写 groups.is_exclusive 的旧版本二进制将无法启动——
-- 本迁移上线即意味着放弃回退到该旧版本的能力。
-- 当前运行的二进制只读 visibility，service.Group.IsExclusive 由 visibility 派生，
-- 不依赖此 DB 列，故删除对运行中的服务零影响。
--
-- 幂等：使用 IF EXISTS，重复执行安全。
-- 普通事务迁移（DROP COLUMN 为元数据操作，不重写表，无需并发索引那套非事务处理）。

-- 1. 删除旧 is_exclusive 索引（ent 历史命名为 group_is_exclusive）。
DROP INDEX IF EXISTS group_is_exclusive;

-- 2. 删除旧 is_exclusive 列。
ALTER TABLE groups DROP COLUMN IF EXISTS is_exclusive;
