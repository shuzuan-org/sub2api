# 支付宝证书目录

把支付宝开放平台下载的**证书模式**凭证文件放在这里，程序启动时会自动加载并优先于 `config.yaml` 的 `alipay` 块与管理后台 Setting 表。

## 需要的文件（文件名必须完全一致）

| 文件名 | 说明 | 从哪获取 |
| --- | --- | --- |
| `appPrivateKey.pem` | 应用私钥 | 生成密钥对时本地保存的私钥（PKCS1 / PKCS8 PEM，或裸 base64 均可） |
| `appPublicCert.crt` | 应用公钥证书 | 支付宝开放平台「开发设置 → 接口加签方式（证书）」上传应用公钥后下载，文件名通常形如 `appCertPublicKey_2021xxx.crt` |
| `alipayPublicCert.crt` | 支付宝公钥证书 | 同页面下载，文件名通常形如 `alipayCertPublicKey_RSA2.crt` |
| `alipayRootCert.crt` | 支付宝根证书 | 同页面下载，文件名通常为 `alipayRootCert.crt` |

四个文件**缺一不可**——任一缺失则视为未通过此目录配置，自动回落到 `config.yaml` / Setting 表。

## 其他参数

`app_id`、`seller_id`（收款账号 PID，可选）、`is_prod`（是否正式环境）仍从 `config.yaml` 的 `alipay` 块读取：

```yaml
alipay:
  enabled: true
  app_id: "2021xxxxxxxxxxxx"
  seller_id: ""      # 可选
  is_prod: true
  cert_dir: "./cert" # 默认值，一般不用改
```

## 安全

本目录内除 `.gitkeep` 和 `README.md` 外的所有文件都被 `.gitignore` 忽略，**不要把证书/私钥提交进版本库**。
