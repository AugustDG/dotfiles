---
name: prompt-smoke
description: Write a prompt that has Codex (or another agent) test a feature the way a real user would, through the product's UI in the built-in browser or its CLI in a terminal, and write a report of what it saw. No code reading, internal API calls or log digging. Reads requirements from a Linear ticket, GitHub PR, or the current branch. Use when asked for a smoke-test prompt, a Codex prompt, a QA prompt, a retest prompt, a user-testing plan for another agent, or to "have Codex test" something. To run the tests yourself, use smoke.
---

# Prompt smoke

Write a prompt that another agent can run on its own. It uses the product only as a user would, through the UI in the built-in browser or the CLI in a terminal, and judges it by what a user can see and do. Then it writes a report that can be acted on without asking it anything.

To run the tests yourself instead, use `smoke`.

The agent is a user, not a developer:
- it doesn't read code, call internal APIs or inspect databases;
- it doesn't read server logs;
- it checks results only in ways a user could.

For a CLI, the commands, their output and the files they write are the user's view. Running a follow-up command a user would run (`list`, `status`, `info`) to check a result is fine.

## Gather the facts first

Before writing, find out:

- **Requirements:** where the expected behavior is defined.
  - A Linear ticket: fetch it with the Linear MCP tools. Take the title, description, acceptance criteria and linked PRs.
  - A GitHub PR URL or number: `gh pr view <PR> --json title,body,headRefName,commits`. Fetch any Linear ticket the body links.
  - Nothing given: try `gh pr view --json title,body,headRefName,commits` for the current branch. If there's no PR, read `git log <default-branch>..HEAD --oneline` and look for ticket IDs (`ENG-123` and similar) in the branch name and commits.
- **Which surface:** the UI, the CLI, or both. Test each one the change reaches.
- **What changed for the user:** the screens, flows, commands and behaviors that are new or different, and what a user should now see or be able to do. Read the diff to learn this, then describe it in the product's own words: tab names, button labels, command names, flags, messages, and where they appear. Don't use file or function names.
- **How to get there:**
  - UI: the local URL, how to sign in, which workspace or account to use, and the path through the UI to each feature.
  - CLI: the exact binary to run (the local build, not a possibly stale global install), how to log in, and which directory to work in.
- **Earlier runs:** look for previous reports, usually under `work/<topic>-<YYYYMMDD-HHMM>/`; the newest is the last run. A retest builds on the last report: it names the failures being rechecked and keeps their exact steps, commands and chat messages.
- **How a user could tell it worked:** for each behavior, something visible. For example:
  - a count shown in the UI, or a command's output and exit code;
  - a row's values against the source page they came from;
  - an exported or generated file opened and checked;
  - a message, banner or state change;
  - the result surviving a page reload, or showing up in a follow-up command.

Check against the code that each expected result you write is what the product really does. Only the expectations go into the prompt, not the code.

## Write the prompt

Use this order. Keep each part short, and give every case an explicit expected result.

1. **Context, in two or three sentences:** what is being tested, where to open or run it, and which earlier reports to read first.
2. **Rules:**
   - Test only as a user would: through the UI in the built-in browser, or the CLI in a terminal.
   - Don't read or change code, call internal APIs, inspect stored data or read server logs.
   - Don't restart or rebuild anything. If the product seems broken or stuck, record what the user sees and move on, or stop and say so.
   - Don't touch real customer data or production. Anything that would delete or change data the agent didn't create is skipped and listed.
   - Where evidence goes: a new `work/<topic>-<YYYYMMDD-HHMM>/` directory, timestamped at the start of the run so a same-day retest never overwrites an earlier one.
   - Use new disposable test accounts, agents or projects, and list them in the report. For a CLI, work in a fresh temporary directory.
   - Judge results by what the product shows and does, and check them against sources a user could open (the store's own pages, the uploaded file, an export, the files a command wrote). Never take the product's word for it when a visible check exists; never take a chat assistant's word at all.
3. **What changed, in the user's words:** a numbered list of what a user should now see or be able to do, and where in the UI or which command.
4. **Preflight:** a quick check that the new behavior is there, such as the new tab or button showing up, or the new command or flag in `--help`. If it isn't, the agent stops instead of testing an old build.
5. **Cases:** cover, at minimum:
   - the main flow the requirements describe;
   - variations: other valid inputs, options, flags and paths to the same feature;
   - edge cases a user can produce: empty fields or arguments, very long or odd input, going back mid-flow, a double click, a missing flag;
   - user errors: bad input should get a clear message (and a non-zero exit for a CLI) and leave nothing half-done.

   Each case has:
   - **Steps as a user takes them:** real clicks, uploads, typing and navigation, or the exact commands to run. For chat, give the exact messages to send.
   - **Expected result**, concrete: what appears, counts, messages, states, output, exit code, files written, roughly how long it takes.
   - **What to record:** screenshots of the key states or the terminal output, the text shown, sample values compared with their source, and any visible errors or odd behavior.
   - **Behavior to observe only:** label it so, such as an assistant's wording, and don't give it a pass or fail.
   - **Out of reach:** states a user can't produce (races, forced failures, expired sessions) don't belong in this prompt. List them for the user to cover another way.
6. **Variety:** when real-world inputs matter (stores, files, sites, projects), have the agent choose them itself. It should vary them, avoid ones earlier runs used, and list its picks with reasons before starting. Name the categories that must be covered, not the specific inputs.
7. **Report:** name the file, `QA-report.md` in the evidence directory, and its sections:
   - **Summary:** one line, saying plainly whether it's a clean sign-off.
   - **Cases:** a table with pass, fail or skipped, and one line each.
   - **Failures:** for each, the steps or commands to reproduce, expected and actual results as seen, and screenshots or captured output.
   - **Before and after:** a table against the previous run, for a retest.
   - **Experience notes:** anything confusing, slow, inconsistent or surprising to a user, even when it isn't a failure.
   - **Coverage limits:** what wasn't exercised, and why.
   - **Evidence index:** what each saved file holds.

## Style of the prompt

- Address the agent directly ("You are testing…").
- Describe the product as a user sees it: screen names, labels, commands, messages. No code paths.
- One expected result per check. "Should work" isn't an expectation.
- Keep it as short as the scope allows. A focused retest of three fixes needs three cases plus a short regression sweep, not the whole original plan.
- Apply the user's prose style rules (for example `unslop`) to the prompt.

## Deliver it

- Write the prompt to the scratchpad directory, for example `<scratchpad>/prompt-smoke-<topic>.md`.
- Copy it to the clipboard (`pbcopy < file` on macOS) unless the user asked for it another way.
- Tell the user in a few lines: what the prompt covers, what the agent will choose itself, where the evidence will go, and what it leaves for testing another way.

## When the report comes back

Read the report, and look at the screenshots and output behind its key claims. Then:

- **Real failures:** confirm each in the code, fix it, and test it.
- **Unmet expectations:** tell real failures apart from expectations the prompt set wrongly, and say which is which.
- **Experience notes:** pass on the ones worth acting on.
- **Coverage gaps:** carry the ones that matter into the next prompt.
- **Next retest:** narrow it to what changed since this run.
