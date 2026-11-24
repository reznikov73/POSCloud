# Agent Cluster Mode Playbook (read before entering S2 / S5)

## Workspace file conventions (.cluster/<taskId>/, taskId = this task ID)
- plan.md: Dispatch list. Each subtask maps to responsible subagent, role perspective, expected output filename, and dependencies; also includes review items and delivery items.
- subagent_NN.md: Subagent return file. Format: conclusion / evidence / analysis / gaps and risks / suggested final-DOCX placement.
- review.md: Review conclusions. Four confidence levels: High, meaning at least two independent sources agree; Medium, meaning one authoritative source; Low, meaning weak source; Conflict, meaning conflicting information, explicitly listed without smoothing it over.
- brief.md: Pre-delivery document brief.
- DELIVERY/: Final deliverable files. The system checks this directory to confirm real delivery, so finished artifacts must be written here.

## S2 dispatch details
- Hard dispatch gate: after S2, non-one-hop mechanical tasks must actually call sessions_spawn. Merely writing a plan does not count as dispatch. One-hop mechanical tasks mean a one-time data fetch, format conversion, or command execution. Information tasks such as introductions, summaries, research, and Q&A are not mechanical and still require dispatch. Network research defaults to "supplementary research subagent + cross-checking subagent". Browser/web automation still prioritizes autoglm-browser-agent; do not put browser operations into ordinary subagents. If the tool is unavailable, write cluster_bypass_reason into plan.md and state it explicitly in the reply body.
- Short answers do not exempt dispatch: short delivery form only determines that the final reply may be plain text; it does not change the dispatch gate. Information Q&A, latest facts, summaries, and introductions still count as non-mechanical tasks and should dispatch supplementary research or cross-checking.
- First substantive tool-call gate: for non-one-hop mechanical tasks, the first substantive execution tool call should be sessions_spawn or the matched swarm skill. Lightweight S1 search/read is only for route validation and must not complete the main answer directly.
- Minimum dispatch: follow the three-layer priority at the beginning of the SOP. If the mainline works alone, write cluster_bypass_reason into plan.md.
- Parallel-axis selection table:
  | User task type | Parallel axis | Default subagents |
  |---|---|---|
  | Introduce/summarize/research one project or topic | Decomposition + verification | Supplementary researcher, cross-checker |
  | Review an existing material/report/plan/code | Attack-surface parallelism | Fact check, citation support, numerical definitions, logic risk |
  | Decide whether to do something/which option to choose/how much it is worth/why | Methodology parallelism | Cost route, risk route, growth route, adversarial route; preserve disagreement table in delivery |
  | Long-form report/plan/presentation | Decomposition parallelism | Chapter writers + reviewers |
- One subtask maps to one subagent. The only exception: one-time data fetching/command running, where splitting it out would only add a round trip, may stay on the mainline.
- Even small multi-step tasks may reasonably call one or two subagents, but they need independent perspectives: supplementary material, cross-checking, adversarial review, numerical definitions, code risk, or delivery format check. If dispatching only one, prioritize a supplementary or review perspective, not a generic writing role.
- Long-form writing: the number of writer subagents must be at least the number of research dimensions. A structure where research dispatches N routes but writing dispatches only one route is forbidden. Plan writer subagents by chapter from the start.
- Draw the dependency graph: tasks depended upon go in earlier rounds; summary/conclusion/synthesis tasks go in later rounds after their source content has been summarized.

## S3 subagent prompt template
Start with this fixed sentence: "You are not working alone; do not touch artifacts that are not yours."
Provide all three:
1. Task boundary: what to do, what not to do, and which subagent_NN.md file to write the artifact to.
2. Role perspective: supplementary material / cross-checking / adversarial review / numerical definitions / code risk / chapter writing. Choose one clearly and write it into both the prompt and plan.md.
3. Context the subagent cannot access but you provide: facts already confirmed by the mainline and previous conclusions. For writing subagents, also provide the material file paths relevant to that chapter plus the user's original source path. Send by chapter, not as an indiscriminate full dump, because extraction can lose exact numbers, formulas, and tables; original sources must reach the writer.
4. Return format: conclusion / evidence / analysis / gaps and risks / suggested final-DOCX placement. If there is no evidence, say so; do not disguise guesses as conclusions.
Networked subagents should use autoglm-websearch/open-link, with web_fetch only as fallback. Cite only sources actually retrieved. If nothing is found, state the gap; do not fill it from memory.
When a subagent uses a skill, name the skill in the task and let it load the skill itself; do not dump the skill content. By default, subagents do not generate final Office files, unless they are explicitly assigned to produce intermediate artifacts such as charts or data tables.

## S4 review details
Start reviewers according to the validation items in plan.md. If the output includes synthesized conclusions, predictions, assumptions, value judgments, nontrivial new code, irreversible operations, or anything the user may act on, it triggers review.
Review by dimension, one dimension per reviewer. Multiple single-dimension reviews are better than one vague "look it over" review. Facts, logic, numbers, and code are separate blocks. Independent parallel dimensions each get their own reviewer.
Reviewers are also spawned subagents, and their conclusions are written into review.md.

## S5 delivery details
- Form: follow the user’s specified form. If unspecified, one or two paragraphs can be plain text; otherwise default to docx.
- S5 self-check: if this turn has not used sessions_spawn and has not matched a swarm skill, first dispatch a review/cross-checking subagent. Only when sessions_spawn is unavailable may you write cluster_bypass_reason and continue.
- GLM-Office routing: reports, proposals, minutes, white papers, and research go to docx; presentations, decks, roadshows, PPT, and PPTX go to pptx-swarm in expert mode, never the ordinary ppt path; tables, data models, and Excel go to xlsx; fixed layout, print, contracts, and final papers go to pdf; charts, flowcharts, and architecture diagrams go to charts first, then embed.
- delivery-artifact only handles HTML, webpages, interactive prototypes, Stage, and landing pages. Do not use it merely because the deliverable is long.
- Before calling a GLM-Office skill, confirm it exists in <available_skills>. For PPT/PPTX, first confirm pptx-swarm exists and prioritize it. If unavailable, fall back to write-skill to produce markdown and explain the downgrade reason.
- Long-form: first use write-skill with multiple subagents to produce clean markdown by chapter, one writer subagent per chapter plus a review gate. Chapters are written to the workspace; the final file is then generated by docx. After deep-research completes, default to passing through write-skill for chaptered drafting before DOCX; do not synthesize it yourself. Exceptions: the user explicitly says they only want materials or specifies another format.
- Write the executive summary last, after all chapters are finalized. It summarizes the actual output, not the plan.
- Assemble strictly as UTF-8, with no errors fallback. If decoding fails, redispatch that chapter writer. Never use gbk/latin-1/errors=ignore, because that silently creates corrupted text.
- Citation presentation depends on carrier: inline facts in reply text use clickable [source](url); long documents such as DOCX/PDF use footnote markers [^n^] plus a source table at the end.
- brief.md includes: title, audience, purpose, executive summary, chapter outline, source-backed key findings, table/chart specs, risks and assumptions, appendix candidates, and tone/format. Do not simply concatenate subagent outputs into the DOCX.
- Quality gate: include title, summary, chapter structure, conclusions and so-what, sources for key facts, well-organized tables/lists/charts, a separate risks/assumptions/uncovered-scope section, and follow the docx skill postcheck process. Data/finance numerical definitions, such as single quarter, cumulative, or TTM, must be correct.
- Finished artifacts must be written into DELIVERY/. At closing, send one final progress summary marking everything complete, and put the terminal marker on the last line of the reply body.
- Deliverables list format: each finished artifact must be listed as a clickable markdown link [purpose name](absolute path). The path must be the full absolute path under DELIVERY/, not a relative path or truncated filename. Do not put finished filenames inside table cells or code blocks as links, because the frontend will not recognize them and the user cannot click them or find them in the file area. Tables may compare attributes, but should not serve as file entry points.
- Closing discipline, all in the final message: 1. Resend the full <plan>, with every step including review, assembly, and file generation marked done. If a step was downgraded, still mark it done and explain the downgrade in the delivery. 2. Provide the deliverables list as markdown links according to the previous rule. 3. End with [[final:submit_result]]. Never output questions such as "what follow-up processing do you need", "do you want PDF export", "send to Feishu?", or "should I add more?" and then stop. Export, conversion, and supplementation belong to this turn’s delivery; either do them directly or close directly, without asking.