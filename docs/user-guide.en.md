# Team Cross user guide

[简体中文](user-guide.md) | **English**

[Back to README](../README.en.md) · [Documentation index (中文)](README.md) · [Development and validation (中文)](development.md)

This guide covers installation, sharing, joining, native clients, MCP, and resuming work. See the [README](../README.en.md) for the product overview and current downloads. Version-specific changes and upgrade limitations are documented in [GitHub Releases](https://github.com/YTwsy/Team-Cross/releases).

- [Install and upgrade](#install-and-upgrade) · [Open and stop the service](#open-team-cross)
- [Share and review](#share-and-review) · [Continue with shared execution](#continue-with-shared-execution) · [Invitations and joining](#invitations-and-joining)
- [Claude Code native TUI](#claude-code-native-tui-experimental) · [Models and reasoning effort](#models-and-reasoning-effort)
- [Personal agents and MCP](#personal-agents-and-mcp) · [Reading and annotations](#reading-and-annotations)
- [Library and quick view](#library-and-quick-view) · [End and resume sharing](#end-and-resume-sharing)

In this guide, **A** means the host and their Mac, and **B** means one invited participant. A space can have multiple invited participants, each with an independent member identity.

## Install and upgrade

Team Cross supports Apple Silicon Macs running macOS 14 or later. See [Install](../README.en.md#install) for the current stable `v0.2.5` DMG, CLI, and Homebrew options. Installed releases do not require Go, Node, or pnpm.

The app bundles the full CLI and Core service and does not require Homebrew. The Cask installs both the app and the `teamcross` command; the Formula provides the standalone CLI. The app and CLI packages, including stable and RC Homebrew definitions, occupy the same app or command paths, so choose one installation method. RC packages must be installed explicitly; an ordinary `brew upgrade` does not switch a stable channel to RC.

After installing from a DMG, choose **Command Line Tool…** in the menu bar to install `teamcross`. The default command path is `/usr/local/bin/teamcross`, with system authorization requested when needed. Existing commands installed by Homebrew or another source are not overwritten. Updating the app at the same location makes the command use its new bundled CLI. Removing the command does not prevent you from using the app. If you use a custom shell, make sure the command directory is in your `PATH`.

Quit Team Cross before upgrading or switching channels, then update or uninstall through the original channel. Installation preserves collaboration data and working directories, but `v0.2.5` does not load or migrate old `schema:2` material; that data remains on disk. See the [release notes](https://github.com/YTwsy/Team-Cross/releases/tag/v0.2.5) for details.

To switch from the Formula to the app, stop the service and run either `brew uninstall --formula --force teamcross` or `brew uninstall --formula --force teamcross-rc`, depending on the installed channel, then install the Cask. To switch in the other direction, use `brew uninstall --cask team-cross` or `brew uninstall --cask team-cross@rc`. DMG users should remove the command through the app's menu before removing the app. These uninstall operations preserve collaboration data and working directories.

The stable definitions `YTwsy/teamcross/teamcross` and `YTwsy/teamcross/team-cross` are maintained separately from RC definitions and may refer to different versions. Use the stable installation options in the README for the current material and discussion features. Consult the corresponding release for each channel's version and installation status.

The current package is ad-hoc signed, without a Developer ID signature or Apple notarization. If macOS blocks the first launch, try opening the app, then follow [Apple's instructions](https://support.apple.com/en-us/102445) to allow it in **System Settings → Privacy & Security**. The build manifest records each package's signing and notarization status.

Reading published material does not require Codex or a local repository. To start a collaboration, operate a shared session directly, or use MCP assistance, follow the interface's instructions for the corresponding Codex or Claude Code client. See [native clients and models (中文)](agent-wiki/wiki/concepts/native-clients-and-models.md) for supported entry points and the [validation index (中文)](agent-wiki/wiki/concepts/validation-gates.md) for actual coverage by version.

## Open Team Cross

Open the menu bar app, or run:

```sh
teamcross serve
```

Both `teamcross` and `teamcross serve` start or reuse the background service, open the browser, and return control to the terminal. You do not need to pass a collaboration repository; the selected source session determines the execution directory. Opening Team Cross again does not create another Core. Closing the browser or terminal does not stop the service.

Copies of the current app running under the same user and data directory also share one menu bar entry. Opening another copy forwards its page-opening or invitation-preview request to the existing app. The second copy exits without stopping the collaboration service. Follow the installation instructions and quit the old app before upgrading.

The default data directory is `~/Library/Application Support/Team Cross Next`. Only one Core runs for a given user and normalized data directory. If the default port `43210` is occupied, Team Cross selects an available port; use the address printed at startup. A conflict with an explicitly supplied `--listen` address is an error. Advanced options include `--data-dir`, `--no-open`, `--codex-bin`, `--claude-bin`, and `--desktop-app`. The `--repo` option only affects local assistant context; it does not select the shared repository.

```sh
teamcross status --json
teamcross doctor --json
teamcross cli-status --json
teamcross stop
# For active collaborations, first confirm the impact of interruption:
teamcross stop --force
```

Choosing **Quit Team Cross** from the menu bar stops the local service, with confirmation if collaborations are active. Stopping B's service only disconnects B; it does not end A's runtime. Sessions, code, and worktrees are preserved. Installing an update does not automatically replace a running Core. If versions are incompatible, stop the service using the original version first.

## Share and review

1. On the home page, choose **Start a space → Share and discuss first**, search for and explicitly select a local session, then click **Choose published range**. This step freezes local history only: it does not create a fork, require a Git directory, or upload content.
2. Use the conversation directory to locate text, then select **Start from this round** and **By the end of this round** on the desired turns. You can also choose **All completed turns** or **Last 3 rounds**. A turn's prompt, answer, and saved tool activity are shared together, with a continuous range between the start and end. Clicking the directory does not change the range. **Suggest colleagues to start reading from this round** only sets a reading recommendation. If narrowing the range excludes that turn, the recommendation is cleared and a notice appears.
3. The range summary and reading controls stay at the top as you scroll. Click **Preview shared content**, set the title on the confirmation page, review the selected text, tool content, and export notes, then choose **Create a read-only space and publish it**. Expand the export-note count to see what it means; specific omissions are explained in the corresponding turns, and the count is not a count of missing turns. Long tool output can be read on demand, and collapsed content is still part of the published range. Going back to adjust the range preserves your reading position and expanded content; the confirmation page always starts at the beginning of the shared range. Active turns and later content are not automatically published.
4. Choose LAN or Tailcat and generate one invitation link that multiple teammates can use. Members can choose **Publish session materials** to contribute one or more sessions of their own. Annotations and replies can reference fixed versions of multiple pieces of material.
5. The body is loaded when you choose **Reading materials**. Personal agents first use `list_materials`, then `read_material`. The default response includes the full conversation with long tool output collapsed; use the returned `turnId + itemId` to page through an individual output when needed. **Publish new version** defaults to the author's previous published range and requires another preview. Old annotations continue to reference the old version. Withdrawing material prevents further reads but cannot recall copies already read or historical references.

A read-only space does not grant native operation or working-directory access. When execution is needed, the host chooses **Enable shared execution**, then selects the source, directory, and fixed permission mode. The space keeps its original link, members, material, and discussions. Use **Open execution access** in the member list to share the full native history and working directory with specific people, then hand over input separately. The original read-only invitation still grants only material and discussion access.

A read-only space always provides **Close space**, even before an invitation is generated or anyone joins. Its closed state is saved, while material and discussions are preserved. Use **Reopen space** to make it available again.

You can also publish through your personal agent or the terminal. Use `get_current_source` to verify the current session's identity, or explicitly select a source from the list; do not guess the most recent session. Replace all IDs, turn boundaries, and hashes below with the actual values returned by the preceding steps:

```sh
teamcross sources --provider codex --json
teamcross freeze --provider codex --source <source-uuid> --json
teamcross publication-preview --draft <draft-id> --title 'API investigation' --start-turn <first-turn-id> --end-turn <last-turn-id> --json
teamcross space --title 'API investigation' --request-id <new-space-uuid> --json
teamcross publish --id <space-id> --preview-id <preview-id> --preview-hash <preview-hash> --request-id <new-publication-uuid> --json
teamcross invite --id <space-id> --transport lan --request-id <invitation-operation-id> --json
teamcross materials --id <space-id> --json
teamcross read-material --id <space-id> --material <material-id> --version 1 --json
```

If the publication result is uncertain, query `publication-status --id <space-id> --request-id <original-publication-uuid>`. An explicit retry keeps the original request ID and preview. To update material, add `--material <material-id> --base-version <current-version>`; existing versions are not overwritten.

## Continue with shared execution

1. On the home page, choose **Start a space → Execute directly together**, search for a Codex or experimental Claude Code source, and select an existing session with completed conversation turns.
2. Choose the execution directory, collaboration mode, and transport. Review the starting point, edit the generated name if needed, and select **Create and invite**. Choose explicitly between LAN and experimental Tailcat; there is no automatic fallback or switching. If sharing fails, the created collaboration is preserved. Retry the invitation from its details without creating another fork.

| Directory mode | What happens at creation | Where subsequent code runs |
| --- | --- | --- |
| Use the original directory | Preserve its Git branch, staging area, and existing files; create only a new session fork | The source session's original directory |
| Create a new worktree | Check out a separate directory from the confirmed `HEAD` and create a `codex/collab-<short-id>` branch; staged, unstaged, untracked, and ignored content is not copied | The corresponding source subdirectory inside the new worktree |

The collaboration mode is fixed at creation and reused on resume; there is no mode switch afterward. **Restricted mode** is the default and keeps Team Cross's tool and permission restrictions. **Trusted mode** supports Codex and experimental Claude Code and inherits the host's native configuration and permissions, including MCP, plugins, hooks, network access, commands, and any browser or computer-control tools already enabled natively. Tools use the host's service authorizations and may access data outside the working directory. Loading personal tools during creation may also trigger hooks. Trusted mode inherits what is actually available and still follows native-client, organization, and system policies; it does not enable tools that are absent or unauthorized. Details and invitation previews display the mode. See [collaboration modes (中文)](agent-wiki/sources/decisions/runtime-modes.md) for the full boundaries.

Creation does not send a task prompt. If the source session or Git starting point changes, review the starting point again. Ordinary Git repositories are supported; repositories with submodules are not supported yet.

You can also start from your personal agent's Team Cross tools or the terminal. Select an explicit local source, preview the starting point, then create and invite:

```sh
teamcross sources --provider codex --search 'session name' --json
teamcross preview --provider codex --source <source-uuid> --workspace existing --json
teamcross share --provider codex --source <source-uuid> --workspace existing \
  --request-id <requestId-from-preview> --preview-hash <previewHash> --transport lan --json
```

Use `--workspace worktree` for a separate working directory and `--runtime-mode trusted` to explicitly choose trusted mode. Preview and creation must use the same settings; restricted mode is the default. `--provider claude` selects the experimental Claude source independently of the personal client calling the tool. `sources` supports pagination with `--cursor`. These commands start Core on demand without opening a browser.

You can tell a personal agent connected through MCP: “Share the current session using the original directory, restricted mode, and LAN.” The tools verify the current session identity supplied by the native client, preview the current state, and register the sharing request. Core creates the fork and invitation **after the current response finishes**, so the fork includes the complete turn. The agent returns a request ID first. On the next turn, use `get_share_request` to check the result and `create_invitation` to retrieve the invitation after success. Registering a request does not mean the invitation already exists; do not ask the agent to poll for its own turn to finish within that same turn.

Use `teamcross share-status --id <request-id> --json` to check from the terminal, or `teamcross cancel-share --id <request-id>` to cancel a request that is still waiting. Once creation starts, its actual result is preserved. A change to the source turn or Git starting point stops the operation instead of silently choosing a new starting point. Unfinished requests are marked interrupted when Core stops and are not automatically created after restart. Clients whose native caller identity cannot be verified can still use the explicit-source flow above; “most recent session” is not a substitute for “current session.” See [personal agent and CLI validation (中文)](agent-wiki/sources/validation/agent-cli-collaboration-2026-09-16.md) for version-specific evidence.

`create` only creates the collaboration. `invite --id <collaboration-id> --transport lan|tailcat` only generates or retrieves the current reusable invitation. `share` performs both in sequence. An invitation failure returns the created collaboration and recovery instructions; retry only `invite`, without creating again under a different request ID. Retrying creation with the same request ID reuses the existing collaboration. If the result is uncertain, query `collaborations --id <requestId>` first. Invitation links and codes are returned for you to share; they are not automatically sent to teammates.

After a Codex collaboration is created, the host can click **Open in personal Codex** at the top of its details to locate the new session without searching the Desktop list. Creation, teammate conversations, and input handoffs do not automatically switch your page. This entry opens your usual Desktop; while sharing is active, continue using direct clients or assistant tools for operations. Once sharing ends and the session is released, the entry becomes **Continue in personal Codex**. A successful system open request does not prove that new conversation content has synchronized in real time.

## Invitations and joining

The host receives an invitation containing an app link, invitation code, and installation instructions. Participants with Team Cross installed can open `teamcross://join?invite=…`, review the collaboration name, host, and access scope, then enter the context page. Others should install Team Cross and open the link again. You can also paste the raw code or app link into **Join a space**. Explicit CLI joining starts Core and opens the details page:

```sh
teamcross join 'tcx3.…'
```

Use `teamcross inspect-invitation --stdin --json` to inspect the declared name, host, and mode, then `teamcross join --stdin --no-open --json` to join. Enter the same invitation separately for each command. Preview only parses local content; joining verifies the remote host. The join result includes the local collaboration `id`. The corresponding personal MCP tools are `preview_invitation` and `join_collaboration`.

Invitations use temporary TLS certificates and a host fingerprint. The same link can admit multiple people until it is closed, reset, or sharing ends, and each member receives independent credentials. Closing a client or temporarily disconnecting does not revoke membership. B reconnects using saved credentials after restarting Core. After leaving voluntarily, B can rejoin through a still-valid link. If A ends sharing or stops or restarts Core, A must reopen sharing and generate a new link. The host must keep Team Cross running.

Connection requirements are explicit:

- **LAN:** Participants must be mutually reachable on the same local network. The connection does not depend on public Tailcat services.
- **Tailcat, experimental:** Neither participant needs a Tailscale account or a system TUN. Tailcat uses DERP for discovery and the initial connection, then peer-to-peer UDP when possible; it can otherwise continue through DERP relay. Reachability and quality depend on both networks and the selected DERP. The invitation includes the Tailcat address, pre-shared key, Team Cross invitation secret, and TLS fingerprint, so treat it as a secret.

Both transports use the same invitation flow, independent member credentials, TLS 1.3, and SPKI fingerprint verification. Team Cross does not silently try LAN and fall back to Tailcat. Recorded network tests on a single machine cover Tailcat connection setup, joining, member reconnection, and the direct-client WebSocket bridge. They do not establish acceptance across two Macs, different networks, or forced DERP relay. See [Tailcat validation (中文)](agent-wiki/sources/validation/tailcat-transport-2026-09-15.md).

Members of a read-only space can read, publish material, and participate in discussions. Once execution is enabled and you receive execution access, you can also use these entry points:

- **Operate directly:** After receiving control of input, use a local Codex TUI or a separate Codex Desktop dedicated to the collaboration. The client connects to the same shared fork, with execution on the host Mac. Each participant keeps one direct client for a collaboration; close the current direct client before switching. Claude collaborations use the experimental native TUI.
- **Assist with your own client:** Choose personal Codex TUI/Desktop or Claude Code TUI, keep your ordinary session, and use Team Cross tools to select the collaboration, read history, inspect files and changes, provide input, or add annotations. These sessions keep their own local context.

Multiple teammates join separately through the same link. The host can choose **Close invitation link** or **Reset invitation link**, neither of which affects existing members, or remove a member individually. Anyone holding a valid link can forward it or rejoin. Removing a member revokes their current credentials; preventing them from rejoining also requires closing or resetting the link. Input is handed to a selected member. Removing the current input holder returns control to the host without automatically interrupting the model, while other members continue participating.

Each participant's Core maintains online status through heartbeats; closing a browser is not treated as leaving. An invited participant can request or cancel a request for input. The host explicitly hands over or reclaims input, and the participant can return it. Requesting input does not automatically transfer control or start the model. Direct-client and assistant-tool writes share the same input-ownership checks, while reads can happen in parallel. Codex collaborations support follow-up input, interruption, and approval responses through native clients or MCP; Claude collaborations use the native TUI for those operations.

The interface distinguishes a launch request, a connected client, and a ready shared session. A connected client does not necessarily mean the session is open. On first launch, the dedicated Desktop may require login or skipping onboarding, followed by opening the collaboration session from the sidebar. It uses a separate app data directory from your existing Desktop. Account login and client preferences are handled locally, while the shared session makes model calls through the host runtime. This entry supports the collaboration's history, input, approvals, and code operations; other global Desktop features are outside the initial compatibility commitment.

## Claude Code native TUI (experimental)

Select the experimental Claude Code provider on the creation page. Both A and B need Claude Code `2.1.268` or later. That version is the tested baseline for the older implementation; later stable versions are not rejected simply for having a different version number. Configure the CLI path in settings or pass `--claude-bin` at startup. A native background job on A performs execution, while the current input holder's native TUI handles input, approvals, and interruption. Reading history, creating a fork, and resuming do not send task prompts.

Claude collaboration creates a new session with native `--fork-session`. In trusted mode, Claude manages the new fork directly in the personal configuration directory. In restricted mode, after the first task input is persisted, Team Cross publishes only that new fork's transcript into `projects` under A's current personal Claude home. It appears in Claude Code CLI/TUI `/resume` like an ordinary native fork and can be continued from personal history after sharing ends. A fork with no input may not yet be saved and is not guaranteed to be resumable after release; Team Cross does not treat that as a creation failure.

In restricted mode, each collaboration uses its own `claude-runtime` worker configuration directory, containing only the selected source's byte-for-byte snapshot, the new fork, restricted settings, MCP configuration, an authentication snapshot, daemon/job data, and ownership state. The worker does not receive the entire personal `projects` directory. Team Cross does not fabricate provider JSONL or target personal settings, plugins, existing history, or authentication files for writes. In restricted mode, it only adds the fork the user explicitly created to personal history. Native tools in trusted mode may modify personal resources when authorized. B's direct TUI does not save A's provider history. Claude Desktop, Web, and Cloud history are outside this feature's scope. Older collaborations using a separate `claude-home` are not migrated and must be recreated.

The WebGUI and assistant tools can read persisted context, add annotations, and send text while idle. Claude follow-up input, interruption, approvals, and model selection currently use the native TUI. The shared worker includes tools for reading and replying to the current collaboration's annotations. Trusted mode creates and runs the fork in the host's native configuration directory, reusing personal MCP, plugins, hooks, permissions, and available tools; restricted mode keeps those extensions disabled. Claude Desktop is not integrated, and Claude's permission system is not equivalent to Codex's OS permission isolation. Claude uses A's API routing. OAuth and Keychain login flows have not been validated. See the [Claude integration contract (中文)](agent-wiki/sources/decisions/claude-native-tui.md) and [September 14 personal CLI/TUI history validation (中文)](agent-wiki/sources/validation/claude-personal-history-2026-09-14.md). The [September 12 test record (中文)](agent-wiki/sources/validation/claude-native-tui-2026-09-12.md) describes the older implementation before that change.

## Models and reasoning effort

Creation inherits the source session's saved model, provider, and reasoning effort. If that information is missing, it uses A's Codex configuration. Later selections in the native client are preserved by the collaboration gateway. Tools that omit those settings use the shared session's current configuration.

Resuming reads the collaboration session's latest persisted settings. **Technical details** shows the current model and reasoning effort confirmed by Codex; offline values are labeled as the last confirmed model. Users configure the local model of their ordinary assistant TUI/Desktop themselves. `gpt-5.6-luna` is used only for this repository's real-model tests.

## Personal agents and MCP

In **Settings and Connections** or **Use your own client to assist**, select Codex or Claude Code and connect the corresponding local client. You can also use the exact command shown on the page:

```sh
codex mcp add teamcross -- /absolute/path/to/teamcross mcp
claude mcp add --transport stdio --scope user teamcross -- /absolute/path/to/teamcross mcp
```

For a custom data directory, append `--data-dir /absolute/path` after `mcp`. Configuration uses a stable Homebrew `opt` path or an absolute path inside the installed app. Codex TUI and Desktop share configuration. Claude writes to personal user-scope configuration; reconnect existing clients through `/mcp` or reopen them. Settings separately displays configuration, protocol probing, and evidence of actual tool calls from Codex or Claude. An MCP handshake does not start Core; the first tool call can start it without a browser.

Personal Claude and Codex clients use the same Team Cross tools and can assist collaborations using either provider. Actual write capabilities depend on the target collaboration and current input ownership. Personal sessions run locally and make their own model calls; tasks sent to the shared session execute on A. See [personal Claude MCP validation (中文)](agent-wiki/sources/validation/claude-assist-2026-09-12.md). Project configuration with the same name, explicit disabling, or organization policies may affect tool loading, and the settings-page checks do not cover other directories.

You can ask your client: “Use Team Cross to list collaborations, then inspect this collaboration's context and current status.”

### Common tools

Common personal MCP tools are listed below. See the [protocol (中文)](agent-wiki/sources/protocol.md) for parameters and the full interface.

| Tool | Purpose |
| --- | --- |
| `freeze_source_session` / `read_publication_draft` / `preview_publication` | Freeze local history, inspect a private draft, and preview the selected published range |
| `create_readonly_space` / `publish_material` | Create a read-only space and publish confirmed material without an execution fork |
| `get_publication_status` / `withdraw_material` | Check a publication result or withdraw your own material to stop future reads |
| `list_materials` / `read_material` | List space material and read fixed versions on demand |
| `read_selection` | Read selected material, annotations, and context using a local library ID; continue with `nextOffset` |
| `list_collaborations` | Locate collaborations started or joined locally |
| `get_collaboration` | Inspect the execution host, directory, input holder, and runtime state |
| `list_source_sessions` / `preview_collaboration` | Choose a local source and preview the directory and fixed collaboration mode |
| `create_collaboration` / `create_invitation` | Create a fork from the confirmed starting point, then separately generate or retrieve an invitation |
| `get_current_source` / `preview_current_share` / `share_current_session` | Verify and preview the current session, then register sharing after the current turn ends |
| `get_share_request` / `cancel_share_request` | Check a sharing request or cancel one that is still waiting |
| `preview_invitation` / `join_collaboration` | Preview and join a user-selected invitation |
| `open_client` | Open a new TUI or dedicated Desktop window after receiving input control |
| `end_sharing` / `leave_collaboration` / `resume_collaboration` | End sharing, leave voluntarily, or resume the same collaboration runtime |
| `request_input` / `cancel_input_request` | Request or cancel input as an invited participant, without automatically transferring control |
| `handoff_input` / `reclaim_input` / `return_input` | The host hands over or reclaims input; a participant returns it. Supply the `epoch` from the details |
| `read_context` | Read history, events, files by relative path, current Git changes, and original annotation references |
| `send_input` | Start a turn or add input to the current turn |
| `interrupt_turn` | Interrupt the specified current turn |
| `respond_to_request` | Respond to a native approval or user-input request |
| `add_annotation` | Save general feedback or an annotation with quoted text and a structured position; does not automatically inject it into the model |
| `reply_to_annotation` | Reply to an existing annotation and return it with all replies; does not create nested annotations |

Successful submission and completed execution are different states. After disconnection or a response timeout, check the actual result before retrying; writes are not automatically repeated. Tools do not automatically add participant identity to the model's input.

### Terminal queries and input control

You can query collaborations and manage input without opening a browser:

```sh
teamcross collaborations --json
teamcross collaborations --id <collaboration-id> --json
teamcross input request --id <collaboration-id> --epoch <epoch-from-details>
teamcross input handoff --id <collaboration-id> --member <member-id> --epoch <latest-epoch>
```

`input` also supports `cancel`, `reclaim`, and `return`. Query the details first and pass the input-state version you see. Stale operations are rejected. If a result is uncertain, query it instead of automatically replaying it. The host can only hand input to a member who has joined. Handing over or returning input waits for the current turn to finish; reclaiming input does not automatically interrupt an active turn. A handoff closes the previous direct client, while assistant tools can still read context.

After receiving input, use `teamcross open --id <collaboration-id> --client tui` to open a new terminal window, or `--client desktop` for the dedicated Codex window. `--print-command` only returns the launch plan. Claude supports TUI only. After a successful launch request, check `clientState`; do not assume the session is already open. The host uses `end --id` to end sharing, a participant uses `leave --id` to leave, and `resume --id` resumes the existing fork. These actions preserve native sessions and working directories.

## Reading and annotations

After shared execution is enabled, switch between **Published session materials** and **Collaboration context** in the reading panel's title bar. Content scrolls with the page, while content switching, the directory, and focus controls stay at the top. Switching preserves each view's reading position, opened version, and tool activity. Changing the width in focus mode also tries to preserve the current passage. An annotation's **View original location** opens and locates the corresponding content. Read-only spaces use the same reading interface but only show published material. The current mode appears beside the collaboration title; click it to expand the permission scope.

In narrower layouts, use **Contents** in the toolbar to choose a turn. Wide focus layouts can show a side directory. Member information initially appears in full. In a two-column layout, it collapses smoothly as you scroll into the reading area and expands when you return to the top; you can also expand it manually. In narrow layouts, members appear below the content and do not automatically collapse. Annotations remain available on the right or alongside their original text.

**Reading materials** expands the body inside its material card. Version, source, and actions appear at the top. As you scroll, the sticky toolbar lets you switch material or collapse the current one; previously read versions restore their reading positions. The author's **Publish new version** and **Withdraw** actions appear on the right. When viewing a historical version, its title, published turn count, **Join selection**, and favorite action all refer to that version. Favoriting highlights the star and shows **Favorites**; click again to remove it. A failed save displays the reason beside the button.

History starts with the most recent eight turns. Text is aligned by turn, conversations are returned in full, and long tool output is collapsed by default. Tools can pass `nextCursor` into `read_context` as `cursor` to continue the page or read earlier content. They can also use `pageCursor + turnId + itemId + startOffset` from the response to finish reading one output. The WebGUI can load earlier turns and navigate by the conversation directory. Published material and collaboration context support Markdown, code highlighting, collapsed tool activity, focus mode, and a source-text view. New conversation content is announced first; click to refresh the current reading view.

The annotation panel accepts general feedback. Selecting conversation text, choosing **Annotate this message**, or clicking `+` beside a code line opens an editor showing the reference location and original text. The editor appears alongside the source, or below the message in narrow layouts. Drag across consecutive lines on the same side of the same file to add a multi-line code annotation. Each reference keeps its own draft while the page is open. Closing the editor or pressing Escape preserves that draft; use `⌘/Ctrl + Enter` to save. Saved annotations highlight the original passage and provide an entry to the discussion in place.

Use **Referenced material** in an annotation or reply to search by title or author. The latest versions appear by default, with historical versions expanded on demand. Selected references display removable version labels. The text excerpt is loaded only when you click **Preview**.

Expand **Reply** under an annotation to discuss it in chronological order. Replies have one level and always belong to the original annotation. Human and shared-agent authors are identified separately. Collapsing replies or encountering a save failure preserves the current page's draft. Retries use the same request identifier to avoid duplicate saves. Context refresh preserves drafts; a full browser refresh or leaving the page does not guarantee that they survive.

**View original location** returns to the referenced conversation or code. Code annotations record the file, line numbers, before/after side, content fingerprint, and original excerpt. If the source changes, the excerpt remains and a notice asks you to check it; old line numbers are not treated as current content. Conversation annotations bind to native turn/item IDs and the selected text range and can be located across history pages.

The shared runtimes used by direct Codex TUI, dedicated Codex Desktop, and direct Claude Code TUI already include the current space's annotation and material tools. No personal assistant MCP installation is needed for those sessions. You can ask: “Read the Team Cross annotations and replies, follow the references to published material, expand individual long tool outputs if needed, check the original text, and reply to this annotation.” The four shared tools remain `read_annotations`, `reply_to_annotation`, `list_materials`, and `read_material`, scoped to the current space. Personal assistant clients can additionally publish from their own local sources and read private frozen drafts through `read_publication_draft`.

Saving an annotation or reply does not automatically start or add input to a model turn. The model reads the actual context when the user asks, then decides how to handle it. Shared-agent replies are explicitly attributed to Codex or Claude Code. New collaborations receive these tools automatically; existing Codex collaborations receive them when their runtime is resumed. Existing Claude workers do not have their native launch arguments rewritten, so create a new collaboration to use newly added tools. Claude collaborations that already had the tools at creation keep them on resume.

## Library and quick view

The **Library** in the WebGUI's left navigation gathers content from spaces you started or joined. Use **Recently used**, **Participated**, **Annotated by me**, and **Favorites**, filter by material, annotations, or context, and search titles, sessions, and annotations. Items are grouped by session. Material keeps its published version, while annotations keep their original text and replies. Click a title to read, check an item to add it to the selection at the bottom, or click the star to favorite it. Selections persist across filter and page changes.

Items stay in place while you read, select, or favorite them, while background synchronization updates content and state. Click **Refresh** at the top of the library, or reload the page, to reorder by latest usage.

You can also choose **Join selection** from a material reader, an annotation, or collaboration context. Select multiple content types together, including content from different spaces for your personal agent to analyze. **Generate reading entry** shows the local ID and **Copy read prompt** directly in the bottom selection bar. Copy that prompt into your local personal Codex or Claude Code session connected to Team Cross MCP, and ask it to use `read_selection` to read the group. Expand the detailed prompt and selected content if needed. Explicitly ask the agent to reply to an original annotation when you want it to do so.

Each read entry fixes the selection at the time it is generated. Material is pinned to a version; execution context must still be checked against the current source when read. Editing the selection does not change an existing entry; choose **Regenerate** to update it. IDs expire after seven days and work only in the local data directory that created them. Collapsing the entry does not clear the selection. Reads recheck permissions, so an ID cannot bypass withdrawal or lost access. Personal agents can still be configured in **Settings and Connections**.

When every selected item belongs to the same space, you can also choose **Send to shared session**. Review the target, references, and instructions before confirming. The page sends through the existing input channel. The target must have execution enabled, be online and idle, and have you as its input holder. Cross-space selections or a pending approval prevent sending. Acceptance does not mean execution has finished; inspect the target session if the result is uncertain.

Left-click the menu bar icon to open the collaboration quick view, or press `Control + Option + T` when the shortcut is available. Switch between **Current spaces** and **Resources**. The first shows ongoing collaborations and their state; click to open details. The second shares the browser's selection and supports search, favorites, and read-entry generation. Switching views preserves resource filters and selections. You can pin the quick view as a small floating window; full material and long discussions open in the WebGUI. Lists scroll with the page. **Service and Settings** appears on the filter row, and right-clicking the menu bar icon also opens the service menu for settings, CLI management, and quitting.

## End and resume sharing

When sharing ends for a read-only space, material and discussions remain on the host. Reopen invitations to continue; no model runtime needs to be resumed. Enabling execution or resetting a link preserves member identities within the same sharing period. After sharing has fully ended, joining again creates a new identity. That identity cannot update material published under the old identity, but it can publish new independent material. The native-runtime rules below apply only to spaces with an executable session.

**End sharing** closes teammates' native connections and tool access and returns input to the host. After current execution and approvals finish and dedicated clients close, Team Cross automatically stops that collaboration's background app-server and releases the native session. The session, directory, and code remain. There is no requirement to commit, export, or write a conclusion. You can later open the session in Codex or resume it through Team Cross.

After a service restart, the host can choose **Resume runtime** in collaboration details to continue the same saved fork, without creating another fork or worktree. Invited participants keep their local join record and independent credentials. While the host is still sharing and a participant has not voluntarily left, that participant can reconnect from the collaboration list without entering the invitation again. Team Cross does not automatically delete original directories or collaboration worktrees.
