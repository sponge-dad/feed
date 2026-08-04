# COS 产品学习总结

本次对腾讯云对象存储 COS（Cloud Object Storage）的学习，核心结论是：对于 Feed 这类 UGC 内容平台，体量巨大、读多写少、且包含大体积视频文件的静态资源（头像、图文、视频、封面），不应落入 MySQL/Redis 等数据库，而应统一收纳进对象存储。COS 的本质是一个「以对象（Object）为单元、按 Key 寻址、通过 HTTP 接口读写」的分布式存储服务，它只负责存文件本身与可访问 URL，不承载任何业务元数据（宽高、时长等），这与我们把业务元数据留在关系库、只持久化 URL 字符串的设计思路天然契合。

在接入方式上，我们确认了 COS 的主流用法是「客户端直传 + STS 临时凭证」模式，而非由后端网关代理文件字节流。具体链路为：客户端先向后端 `POST /upload/token` 换取临时上传凭证，再凭借该凭证直接向 COS Bucket 发起 PUT 直传，最后把文件 URL 回写给业务接口（如 `PATCH /users/me` 或 `POST /feeds`），整个过程网关只参与「签发凭证」与「回写 URL」，不占用自身带宽去搬运视频等大文件。临时凭证由腾讯云 STS（Security Token Service，基于 CAM 访问管理）签发，通过 `GetFederationToken` / `AssumeRole` 接口发放，且必须利用 CAM Policy 把 resource 限定到形如 `qcs::cos:{region}:uid/{appid}:{bucket}/{key}*` 的指定前缀，使用户只能写入自己目录下的对象，从而实现最小权限与越权防护。

关于 Bucket 与 Key 的规划，我们学到了对象存储的访问控制层是「Bucket 维度的读写权限 + 对象维度的 Key 命名」组合：Bucket 应设为私有读写（禁止 public-read），而对象的可读性通过「临时签名 URL（预签名）」来按需授予，签名 URL 自带过期时间（如图片 10 分钟、视频 1 小时），从而避免永久链接被爬取。Key 的命名因此非常重要，我们采用了 `{env}/{biz}/{uid}/{yyyyMMdd}/{snowflake}.{ext}` 的规范（环境 / 业务类型 / 用户 ID / 日期 / 雪花唯一 ID / 扩展名），其中 uid 取自服务端登录态而非客户端入参、snowflake 用 `common/idgen` 生成以避免碰撞与遍历，按用户与日期分目录既便于审计也便于清理。下载侧同样由网关聚合层统一调用 `GetPresignedURL` 生成签名 URL 后下发，对裸 key、本桶完整 URL、带签名参数的回传 URL 都可归一化处理，并注意用正则校验防止路径穿越。

在 SDK 与依赖层面，我们明确了两个核心 Go 依赖的分工：`github.com/tencentcloud/tencentcloud-sdk-go/.../sts/v20180813` 负责向腾讯云申请 STS 临时凭证，`github.com/tencentyun/cos-go-sdk-v5` 负责签名 URL、HEAD 存在性校验等桶对象操作；并在实践中注意到一个关键坑点——go-zero v1.7.3 的 `conf.Load` 不会替换 YAML 中的 `${ENV}` 占位符，因此密钥（SecretId / SecretKey）通过环境变量注入时，需要在 `cos.New` 构造处用 `os.Expand` 兜底解析，否则会导致密钥为空、客户端初始化失败。密钥安全是贯穿始终的底线：YAML 中只允许出现 `${COS_SECRET_ID}` 占位符、绝不明文，本地开发密钥放在已 gitignore 的 `scripts/cos-env.sh` 中，含密钥的配置不入库。

最后，我们也梳理了 COS 在安全与演进上的边界：写路径需在业务落库前用永久密钥 `HEAD` 校验对象确已上传（防失效/未上传引用），并对 file_type / file_ext 做白名单、视频大小设上限、对 `/upload/token` 做用户级限流以防护刷；缓存一致性上 COS 对象本身无需缓存，靠签名 URL 有效期天然控频，更新头像等场景通过「生成新 key」而非覆盖旧文件来规避缓存陈旧。后续演进方向包括引入 COS 数据万象（CI）做视频/大图转码与缩略图、高频读场景接入 CDN 配时间戳防盗链以降低签名计算开销、以及定期清理未被业务记录引用的孤儿文件。整体而言，COS 在本项目中扮演的是「可靠的、私有的、按 Key 寻址的文件底座」，所有「如何与腾讯云交互」的复杂度都被收敛在 `app/gateway/internal/pkg/cos` 一处，业务服务只感知 URL 字符串，这正是对象存储解耦存储与业务的最佳实践。
