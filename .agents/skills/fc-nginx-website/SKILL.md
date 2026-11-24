---
name: fc-nginx-website
description: >-
  Hosting-environment rules for AutoClaw's managed static website preview on the
  function-compute "nginx" environment: what the environment can and cannot
  serve, how the entry file decides which files get uploaded, and the size
  limits. Use this whenever a request becomes a hosted website, landing page,
  docs site, or built front-end (React/Vue/Vite dist, Hugo/Jekyll output).
  AutoClaw 受控静态网站预览的托管环境规则（function-compute 的 nginx 环境）：
  能托管什么、入口文件如何决定上传范围、体积上限。凡是要交付可访问网站、落地页、
  文档站或已构建前端产物时使用。
---

# Managed static website hosting on function-compute (nginx)
# 受控静态网站托管（function-compute nginx 环境）

## Managed delivery — the only mode / 受控交付（唯一模式）

This is an AutoClaw-managed delivery. You author files; AutoClaw Main packages
and uploads them. **Do not call any `/functionCompute` API, and do not read,
request, store, or output a JWT.** Creating the function, uploading to LATEST,
and formal publication are not yours to perform — the preview upload is Main's
job, and formal publication belongs to the user clicking Publish.

Do not write `nginx.conf` (a model-authored one is discarded), do not build a
zip, and do not base64-encode anything. Do not guess or assemble a preview
domain: AutoClaw appends the real URL to the conversation when the deployment
reaches a terminal state.

Division of labour: `AUTOCLAW_FUNCTION_COMPUTE_WEBSITE_PROTOCOL` owns the flow —
project identity from `website_delivery_start`, where files go, and the
`projects/projects.json` write that triggers deployment. This Skill owns the
hosting environment: what nginx can serve, how the entry file decides the upload
scope, and the size limits.

本任务属于 AutoClaw 受控交付：你只负责产出文件，打包与上传由 AutoClaw Main 完成。
**禁止调用任何 `/functionCompute` API，禁止读取、索取、保存或输出 JWT。** 创建函数、
上传 LATEST、正式发布都不由你执行；正式域名只在用户点击「发布」后启用。
不要自己写 `nginx.conf`（写了也会被丢弃），不要打包 zip，不要做 base64 编码，
也不要猜测或拼接预览域名——部署到达终态后系统会把真实 URL 追加到会话里。

分工：流程由 `AUTOCLAW_FUNCTION_COMPUTE_WEBSITE_PROTOCOL` 规定（项目身份来自
`website_delivery_start`、产物落点、以及触发部署的 `projects/projects.json` 写入）；
本 Skill 规定托管环境（nginx 能托管什么、入口文件如何决定上传范围、体积上限）。

## What this environment can serve / 环境能力

The `nginx` environment is a **static-file host**: Nginx reads files off disk and
returns them. There is no application process, no request handler, and no build
step on the server.

- Works: HTML/CSS/JS, images, fonts, and any pre-built front-end output
  (Vite/CRA/Vue `dist`, Hugo/Jekyll output).
- Does not work: SSR, API routes, databases, WebSocket servers, cron, or
  anything that needs a long-running process. Build to static output instead, or
  state plainly that this channel cannot host it — never fake a preview.
- Does not load: anything the page pulls from another origin. A strict
  Content-Security-Policy blocks CDN scripts, web fonts, remote images, and
  third-party embeds — every asset must ship with the site (see below).
- Client-side routing works: the managed config already falls back to
  `index.html`, so React Router / Vue Router deep links resolve.

`nginx` 环境是**纯静态文件托管**：没有应用进程、没有请求处理函数、服务端不会构建。
SSR、API、数据库、常驻进程都无法托管——请改为构建成静态产物，或直接如实说明限制。
托管平台还会下发严格 CSP，外链资源一律加载不到，素材必须随站打包（见下文）。
客户端路由无需额外配置：受控 nginx 配置已经回退到 `index.html`，深链可用。

## The entry file decides what gets uploaded / 入口文件决定上传范围

**The site root is the directory containing `entryFile`, and only that subtree is
uploaded.** This is the single most common way to silently lose files.

- `entryFile: "index.html"` → site root is the project directory; everything
  under it ships.
- `entryFile: "dist/index.html"` → site root is `dist/`; **nothing outside
  `dist/` is uploaded**, so a stylesheet or image left at the project root will
  404 in preview.

Rules / 规则:

- `entryFile` must be a relative path inside the project directory pointing at an
  existing file. Absolute paths and `..` segments are rejected.
- A `dist/index.html` / `build/index.html` fallback exists **only** when
  `entryFile` is exactly `index.html` or `index.htm`. Any other name has no
  fallback — it must exist exactly where you said.
- If the entry file is not named `index.html`, it is also copied to
  `index.html` so `/` serves it.
- Keep the site root clean: the only exclusions are `.DS_Store`, `.git`,
  `.svn`, `node_modules`, the deployments bookkeeping file, `.env*`, and
  symlinks. `.ts`, `.vue`, `.map`, and stray source files **do** ship. Nothing
  secret belongs anywhere under the site root.

**站点根目录 = `entryFile` 所在目录，且只有这一棵子树会被上传。** 这是最容易静默丢文件
的一点：`entryFile: "dist/index.html"` 时，项目根下的文件一个都不会上传。
`entryFile` 必须是项目目录内、指向真实文件的相对路径，不能是绝对路径，也不能含 `..`；
只有 `entryFile` 恰好是 `index.html` / `index.htm` 时才会回退查找
`dist/index.html`、`build/index.html`。入口不叫 `index.html` 时会被复制一份为
`index.html`。排除清单只有 `.DS_Store` / `.git` / `.svn` / `node_modules` /
部署记账文件 / `.env*` / 符号链接——`.ts`、`.vue`、sourcemap 都会照传，站点根目录里
不要留源码和任何机密文件。

## Size limits / 体积上限

| Limit / 上限 | Value / 值 |
|---|---|
| Source files under the site root / 站点根文件数 | 5000 |
| Total source size / 源文件总大小 | 100 MB |
| Upload body after base64 / base64 编码后的上传体积 | 50 MB |

Optimize images and drop unused build artifacts before they become the reason a
deployment is rejected. 提前压缩图片、清掉无用产物，避免因体积被拒。

## Every asset ships with the site / 外链素材必须随站打包

The serving platform returns a **strict Content-Security-Policy**, so anything the
page loads from another origin is blocked: a framework or icon set off a CDN,
Google Fonts, remote placeholder images, third-party `<iframe>` embeds, and
requests to external APIs or analytics endpoints. It only shows up after
deployment — the page looks right on your machine, then lands as unstyled text,
empty image frames, and fallback fonts. Never link a CDN "just to get the preview
working": the preview is the deployment.

- Author self-contained pages, and download anything you would otherwise link
  into the **site root** (`assets/`, `fonts/`, `vendor/`), rewriting the reference
  to a relative path. With `entryFile: "dist/index.html"` that means inside
  `dist/` — a file saved at the project root is not uploaded. Take only what the
  page renders (the font weights actually used, not a whole icon library) so
  localizing assets does not run into the limits above.
- The design skeletons and example pages you may copy from are written for local
  viewing, and many of them link Google Fonts or a CDN script. Localize those
  references instead of inheriting them.
- **Check before you touch `projects/projects.json`** — that write starts the
  upload, so it is the last moment a fix is cheap. Search the site root for
  `http://`, `https://`, and protocol-relative `//`, then clear every hit the
  browser would fetch: `<script src>`, `<link rel="stylesheet"|"preload"|"icon">`,
  `src` / `srcset` / `poster`, `<iframe src>`, `url(...)` / `@import` /
  `@font-face` in CSS, and `fetch` / `XMLHttpRequest` targets. Inert hits are
  fine — a link the user clicks, a comment, an XML namespace, JSON-LD. Re-run the
  search after fixing, until only inert hits remain.
- A feature that genuinely needs a third-party origin at runtime cannot work here.
  Say so plainly or replace it with something static; do not ship it broken.

托管平台会下发**严格 CSP**：页面从其他 origin 加载的资源一律被拦掉（CDN 上的框架 /
图标库、Google Fonts、远程占位图、第三方 `<iframe>`，以及对外部 API、统计端点的请求），
而且只在部署后才暴露——本机好看，线上变成无样式文本 + 空图框 + 回退字体。
不要"先挂个 CDN 把预览跑通"，预览就是部署。所以：页面自包含，凡是要外链的素材都下载到
**站点根目录**下（`assets/`、`fonts/`、`vendor/`）并改成相对路径引用——`entryFile` 是
`dist/index.html` 时必须放进 `dist/`，放在项目根不会被上传；只下载真正用到的部分
（用到的字重，而不是整个图标库），别撞上面的体积上限。可复用的 design skeleton /
example 页面是给本地打开看的，里面大量挂着 Google Fonts 和 CDN 脚本，照抄过来必须
一并改成本地文件。**更新
`projects/projects.json` 之前必须自检**（那次写入就是上传信号）：在站点根目录里搜
`http://`、`https://` 和 `//` 开头的引用，把浏览器真的会去取的逐个消除
（`<script src>`、`<link rel=stylesheet|preload|icon>`、`src`/`srcset`/`poster`、
`<iframe src>`、CSS 里的 `url(...)`/`@import`/`@font-face`、`fetch`/`XMLHttpRequest`
目标）；纯文本性质的命中（用户点击的超链接、注释、XML 命名空间、JSON-LD）保留即可。
修完再搜一遍，直到只剩这类命中。真的必须依赖第三方 origin 的功能在这里跑不起来：
如实说明或换成静态实现，不要交付一个坏的。

## Troubleshooting the preview / 预览期排错

- **Blank page or 404 on assets.** Almost always the site root: the entry file's
  directory did not contain the referenced files, or the page uses absolute
  paths that do not exist in the uploaded subtree. Use relative asset paths and
  keep everything under the site root. 白屏或资源 404：基本都是站点根目录问题。
- **Styles, fonts, or images missing without a 404.** An external reference the
  CSP blocked — the browser console shows a CSP violation, not a failed request.
  Download it into the site root as above. 样式/字体/图片没生效但不是 404：外链被
  CSP 拦了，下载到站点根目录里。
- **404 on a specific page.** Client-side routing already falls back to
  `index.html`; a real 404 means the file is not in the uploaded subtree.
- **Changes not visible.** The preview URL serves the latest upload; the stable
  URL only changes after the user publishes. 改动看不到：确认打开的是预览地址。
- **Upload rejected for size.** See the limits above — shrink assets.
