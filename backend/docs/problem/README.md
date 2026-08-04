# problem 目录

> 本目录归档 **Bug 总结文档**。每篇记录一个已排查定位的问题，含现象、排查过程、根因、处置方案与验证方法。AI 在产出此类文档前，必须先阅读 [Bug 总结 SOP](../agent/bug-summary-sop.md)。

## 文档索引

| 文件 | 主题 | 类别 |
|------|------|------|
| [20260731-avatar-upload-not-persisted.md](./20260731-avatar-upload-not-persisted.md) | 头像上传后数据库未更新（COS 桶 CORS 未放行浏览器直传） | B 配置问题 |

> 新增文档后在此表追加一行，并在文档内按 SOP §3 模板编写、补全 `## 关联文档`。

## 命名与编写约定

- 文件名：`YYYYMMDD-<slug>.md`（kebab-case），如 `20260731-avatar-upload-not-persisted.md`。
- 结构：严格遵循 [Bug 总结 SOP §3 模板](../agent/bug-summary-sop.md)。
- 禁止：文档内出现密钥明文；引用代码使用非仓库根相对路径。
- 根因类别：A 代码缺陷 / B 配置问题 / C 混合。

## 关联文档

- [Bug 总结 SOP](../agent/bug-summary-sop.md)
- [文档编写规范](../agent/doc-writing-guide.md)
