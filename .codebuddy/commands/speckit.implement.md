---
description: Execute the implementation based on the task plan
---

## Module Detection

This project is a **backend + frontend** monorepo. Before executing any Speckit workflow, you must determine which module the user is targeting.

**Project structure**:
- `backend/` — Go后端服务 (`.specify/` + `.codebuddy/commands/speckit.*.md` 位于此目录)
- `frontend/` — TypeScript/React 前端 (`.specify/` + `.codebuddy/commands/speckit.*.md` 位于此目录)

**Auto-detection rules**:
- 用户提到 Go、API、gRPC、数据库、服务端、gin、中间件、handler、model → **backend**
- 用户提到 React、组件、页面、UI、样式、前端、TypeScript/TS、Vite、组件库 → **frontend**
- 如果无法确定，主动询问: "这是 **后端(backend)** 还是 **前端(frontend)** 的功能？"

## Switch to the Correct Context

确认目标模块后，将工作目录切换到该模块下:

```
cd backend/   # 或 cd frontend/
```

然后读取并执行该模块的 Speckit 命令 `.codebuddy/commands/speckit.implement.md`，严格遵循其中的工作流指令。

所有 `.specify/` 脚本和模板都位于当前模块目录下，使用相对路径即可（如 `.specify/scripts/bash/setup-plan.sh`）。

## User Input

```text
$ARGUMENTS
```
