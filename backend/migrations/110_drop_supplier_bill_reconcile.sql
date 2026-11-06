-- 月度对账功能下线（2026-11-06），回收 109 建的两张表。上线前已在生产确认两表均为空。
-- 109 文件保留不动：迁移按文件名 + 校验和记账，删改已执行的迁移会让启动校验失败。
DROP TABLE IF EXISTS supplier_bill_lines;
DROP TABLE IF EXISTS account_supplier_bindings;
