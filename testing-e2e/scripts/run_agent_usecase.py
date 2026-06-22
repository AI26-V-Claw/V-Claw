#!/usr/bin/env python3
"""
Run a real V-Claw agent use case through the existing CLI.

This runner does not mock the agent, provider, Google tools, or Telegram.
It calls:

    go run ./cmd/vclaw agent -prompt ... -session ... -channel ... -json

Each step result is appended to one artifact JSON file.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import re
import subprocess
import sys
import time
import uuid
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_USECASE = REPO_ROOT / "testing-e2e" / "usecases"
DEFAULT_ARTIFACT_DIR = REPO_ROOT / "testing-e2e" / "artifacts" / "agent-usecases"
DEFAULT_SUMMARY_FILE = "all-usecases.summary.json"
ENV_PATTERN = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}")
DEFAULT_ENV_FILES = [
    REPO_ROOT / ".env",
    REPO_ROOT / "testing-e2e" / ".env",
]

try:
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
except Exception:
    pass


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")


def load_json(path: Path) -> dict[str, Any]:
    with path.open("r", encoding="utf-8") as f:
        return json.load(f)


def write_json(path: Path, value: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(value, f, ensure_ascii=False, indent=2)
        f.write("\n")


def log(message: str = "") -> None:
    try:
        print(message, flush=True)
    except UnicodeEncodeError:
        safe = message.encode(sys.stdout.encoding or "utf-8", errors="replace").decode(sys.stdout.encoding or "utf-8", errors="replace")
        print(safe, flush=True)


def display_path(path: Path) -> str:
    try:
        return str(path.resolve().relative_to(REPO_ROOT))
    except ValueError:
        return str(path)


def step_separator(step_id: Any) -> str:
    return f"--- {step_id} " + ("-" * 60)


def log_block(text: str) -> None:
    for line in str(text).splitlines() or [""]:
        log("    " + line)


def safe_filename(value: str) -> str:
    name = re.sub(r"[^A-Za-z0-9._-]+", "-", value.strip())
    name = name.strip(".-")
    return name or "agent-usecase"


def expand_vars(text: str, variables: dict[str, str]) -> str:
    def replace(match: re.Match[str]) -> str:
        key = match.group(1)
        if key in variables:
            return variables[key]
        return os.environ.get(key, "")

    return ENV_PATTERN.sub(replace, text)


def require_env(names: list[str]) -> list[str]:
    missing = []
    for name in names:
        if not os.environ.get(name, "").strip():
            missing.append(name)
    return missing


def load_env_file(path: Path) -> list[str]:
    loaded: list[str] = []
    if not path.exists() or not path.is_file():
        return loaded
    with path.open("r", encoding="utf-8-sig") as f:
        for raw_line in f:
            line = raw_line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, value = line.split("=", 1)
            key = key.strip()
            if not re.match(r"^[A-Za-z_][A-Za-z0-9_]*$", key):
                continue
            if key in os.environ and os.environ[key].strip():
                continue
            value = value.strip()
            if len(value) >= 2 and value[0] == value[-1] and value[0] in ("'", '"'):
                value = value[1:-1]
            os.environ[key] = value
            loaded.append(key)
    return loaded


def load_default_env_files() -> list[str]:
    loaded: list[str] = []
    for path in DEFAULT_ENV_FILES:
        loaded.extend(load_env_file(path))
    return loaded


def response_status(response: dict[str, Any] | None) -> str:
    if not response:
        return ""
    return str(response.get("status") or "").strip()


def approval_id(response: dict[str, Any] | None) -> str:
    if not response:
        return ""
    direct = str(response.get("approvalId") or "").strip()
    if direct:
        return direct
    request = response.get("approvalRequest")
    if isinstance(request, dict):
        return str(request.get("approvalId") or "").strip()
    return ""


def user_text_from_response(response: dict[str, Any] | None) -> str:
    if not response:
        return ""
    output = response.get("output")
    if isinstance(output, dict):
        text = str(output.get("text") or "").strip()
        if text:
            return text
    text = str(response.get("message") or "").strip()
    if text:
        return text
    error = response.get("error")
    if isinstance(error, dict):
        return str(error.get("message") or "").strip()
    return ""


def approval_tool_name(response: dict[str, Any] | None) -> str:
    if not response:
        return ""
    request = response.get("approvalRequest")
    if not isinstance(request, dict):
        return ""
    tool_call = request.get("toolCall")
    if not isinstance(tool_call, dict):
        return ""
    return str(tool_call.get("toolName") or "").strip()


def artifact_from_response(response: dict[str, Any] | None) -> dict[str, Any] | None:
    if not response:
        return None
    output = response.get("output")
    if isinstance(output, dict) and isinstance(output.get("artifactRef"), dict):
        return output["artifactRef"]
    for result in response.get("toolResults") or []:
        if isinstance(result, dict) and isinstance(result.get("artifactRef"), dict):
            return result["artifactRef"]
    return None


def summarize_run(run: dict[str, Any], user_message: str) -> dict[str, Any]:
    response = run.get("response")
    summary: dict[str, Any] = {
        "user": user_message,
        "status": response_status(response),
        "agent": user_text_from_response(response),
        "durationMs": run.get("durationMs"),
        "exitCode": run.get("exitCode"),
    }
    if appr_id := approval_id(response):
        summary["approvalId"] = appr_id
    if tool_name := approval_tool_name(response):
        summary["approvalTool"] = tool_name
    if artifact := artifact_from_response(response):
        summary["artifact"] = artifact
    if run.get("timedOut"):
        summary["timedOut"] = True
    if run.get("parseError"):
        summary["parseError"] = run.get("parseError")
    if run.get("exitCode") not in (0, None):
        stderr = str(run.get("stderr") or "").strip()
        if stderr:
            summary["stderr"] = stderr[-2000:]
    return summary


def parse_agent_json(stdout: str | None) -> tuple[dict[str, Any] | None, str | None]:
    if stdout is None:
        return None, "stdout was None"
    text = stdout.strip()
    if not text:
        return None, "stdout was empty"
    try:
        parsed = json.loads(text)
    except json.JSONDecodeError as exc:
        return None, f"stdout was not valid JSON: {exc}"
    if not isinstance(parsed, dict):
        return None, "agent JSON was not an object"
    return parsed, None


def build_agent_command(
    prompt: str,
    session_id: str,
    channel: str,
    usecase: dict[str, Any],
    timeout_seconds: int,
) -> list[str]:
    command = [
        "go",
        "run",
        "./cmd/vclaw",
        "agent",
        "-prompt",
        prompt,
        "-session",
        session_id,
        "-channel",
        channel,
        "-json",
    ]

    flags = usecase.get("agentFlags")
    if isinstance(flags, dict):
        if value := str(flags.get("googleTools") or "").strip():
            command.extend(["-google-tools", value])
        if value := str(flags.get("webTools") or "").strip():
            command.extend(["-web-tools", value])
        if value := str(flags.get("dataDir") or "").strip():
            command.extend(["-data-dir", value])
        if value := str(flags.get("credentials") or "").strip():
            command.extend(["-credentials", value])
        if value := str(flags.get("googleToken") or "").strip():
            command.extend(["-google-token", value])
        if value := str(flags.get("maxIterations") or "").strip():
            command.extend(["-max-iterations", value])

    if timeout_seconds > 0:
        # The subprocess timeout is enforced by Python. This is kept out of the
        # CLI args because the production command has no timeout flag.
        pass

    return command


def run_agent_command(command: list[str], timeout_seconds: int) -> dict[str, Any]:
    started = utc_now()
    start = time.monotonic()
    try:
        completed = subprocess.run(
            command,
            cwd=str(REPO_ROOT),
            text=True,
            encoding="utf-8",
            errors="replace",
            capture_output=True,
            timeout=timeout_seconds if timeout_seconds > 0 else None,
            check=False,
        )
        stdout = completed.stdout or ""
        stderr = completed.stderr or ""
        exit_code = completed.returncode
        timed_out = False
    except subprocess.TimeoutExpired as exc:
        stdout = exc.stdout or ""
        stderr = exc.stderr or ""
        if isinstance(stdout, bytes):
            stdout = stdout.decode("utf-8", errors="replace")
        if isinstance(stderr, bytes):
            stderr = stderr.decode("utf-8", errors="replace")
        exit_code = None
        timed_out = True

    response, parse_error = parse_agent_json(stdout)
    return {
        "startedAt": started,
        "finishedAt": utc_now(),
        "durationMs": round((time.monotonic() - start) * 1000),
        "command": command,
        "exitCode": exit_code,
        "timedOut": timed_out,
        "stdout": stdout,
        "stderr": stderr,
        "response": response,
        "parseError": parse_error,
        "status": response_status(response),
    }


def step_passed(step: dict[str, Any], final_run: dict[str, Any]) -> tuple[bool, str]:
    if final_run.get("timedOut"):
        return False, "agent command timed out"
    if final_run.get("parseError"):
        return False, str(final_run["parseError"])
    if final_run.get("exitCode") not in (0, None):
        return False, f"agent command exited with {final_run.get('exitCode')}"

    expected = step.get("expectStatusIn")
    if isinstance(expected, list) and expected:
        status = str(final_run.get("status") or "")
        allowed = {str(item) for item in expected}
        if status not in allowed:
            return False, f"status {status!r} was not in {sorted(allowed)!r}"

    return True, ""


def normalize_step(step: dict[str, Any] | str, variables: dict[str, str]) -> dict[str, Any]:
    step_number = variables.get("STEP_NUMBER", "0")
    if isinstance(step, str):
        return {
            "id": f"step_{step_number}",
            "name": f"Step {step_number}",
            "prompt": step,
            "kind": "send",
        }

    if not isinstance(step, dict):
        raise ValueError(f"step {step_number} must be a string or object")

    normalized = dict(step)
    normalized.setdefault("id", f"step_{step_number}")
    normalized.setdefault("name", f"Step {step_number}")

    if "send" in normalized:
        normalized["prompt"] = str(normalized["send"])
        normalized["kind"] = "send"
        return normalized

    if "content" in normalized:
        normalized["prompt"] = str(normalized["content"])
        normalized["kind"] = "send"
        return normalized

    if "approve" in normalized:
        approval = variables.get("LAST_APPROVAL_ID", "").strip()
        if not approval:
            raise ValueError(f"step {normalized['id']!r} asked to approve but no previous approvalId was found")
        normalized["prompt"] = f"approve {approval}"
        normalized["kind"] = "approve"
        normalized["approvalId"] = approval
        return normalized

    if "reject" in normalized:
        approval = variables.get("LAST_APPROVAL_ID", "").strip()
        if not approval:
            raise ValueError(f"step {normalized['id']!r} asked to reject but no previous approvalId was found")
        normalized["prompt"] = f"reject {approval}"
        normalized["kind"] = "reject"
        normalized["approvalId"] = approval
        return normalized

    if "revise" in normalized:
        comment = str(normalized["revise"]).strip()
        if not comment:
            raise ValueError(f"step {normalized['id']!r} has empty revise text")
        normalized["prompt"] = "revise " + comment
        normalized["kind"] = "revise"
        return normalized

    if "prompt" in normalized:
        normalized.setdefault("kind", "send")
        return normalized

    raise ValueError(f"step {normalized['id']!r} must contain send, approve, reject, revise, or prompt")


def should_execute_step(step: Any) -> bool:
    if isinstance(step, str):
        return True
    if not isinstance(step, dict):
        return False
    role = str(step.get("role") or "user").strip().lower()
    return role == "user"


def run_step(
    step: dict[str, Any] | str,
    usecase: dict[str, Any],
    variables: dict[str, str],
    session_id: str,
    channel: str,
    timeout_seconds: int,
    dry_run: bool,
) -> dict[str, Any]:
    step = normalize_step(step, variables)

    prompt = expand_vars(str(step.get("prompt") or ""), variables).strip()
    if not prompt:
        raise ValueError(f"step {step.get('id')!r} has empty prompt")

    result: dict[str, Any] = {
        "id": step.get("id"),
        "name": step.get("name"),
        "prompt": prompt,
        "messages": [],
    }

    command = build_agent_command(prompt, session_id, channel, usecase, timeout_seconds)
    log("")
    log(step_separator(result["id"]))
    log("USER >")
    log_block(prompt)
    if dry_run:
        log("")
        log("AGENT < dry_run")
        log_block("(dry-run: agent was not called)")
        result["messages"].append({
            "user": prompt,
            "agent": "(dry-run: agent was not called)",
            "dryRun": True,
            "status": "dry_run",
        })
        result["passed"] = True
        return result

    first_run = run_agent_command(command, timeout_seconds)
    result["messages"].append(summarize_run(first_run, prompt))
    first_response = first_run.get("response")
    first_status = response_status(first_response)
    first_approval = approval_id(first_response)
    first_tool = approval_tool_name(first_response)
    log("")
    log(f"AGENT < {first_status or '(none)'}")
    if first_tool:
        log(f"    tool: {first_tool}")
    if first_approval:
        log(f"    approval: {first_approval}")
    first_text = user_text_from_response(first_response)
    if first_text:
        log_block(first_text)
    if artifact := artifact_from_response(first_response):
        uri = artifact.get("uri") if isinstance(artifact, dict) else ""
        label = artifact.get("label") if isinstance(artifact, dict) else ""
        log(f"[artifact] {label or artifact.get('kind', 'artifact')}: {uri or artifact.get('id', '')}")
    if first_run.get("parseError"):
        log(f"[parse error] {first_run.get('parseError')}")

    auto_approve = bool(step.get("autoApprove", usecase.get("autoApprove", False)))
    max_auto_approvals = int(step.get("maxAutoApprovals", usecase.get("maxAutoApprovals", 0)) or 0)

    current = first_run
    approvals_used = 0
    while (
        auto_approve
        and response_status(current.get("response")) == "approval_required"
        and approvals_used < max_auto_approvals
    ):
        appr_id = approval_id(current.get("response"))
        if not appr_id:
            break
        approvals_used += 1
        approve_prompt = f"approve {appr_id}"
        log("")
        log("USER >")
        log_block(approve_prompt)
        approve_command = build_agent_command(approve_prompt, session_id, channel, usecase, timeout_seconds)
        approve_run = run_agent_command(approve_command, timeout_seconds)
        approve_run["approvalFor"] = appr_id
        result["messages"].append(summarize_run(approve_run, approve_prompt))
        current = approve_run
        approve_response = approve_run.get("response")
        approve_status = response_status(approve_response)
        approve_approval = approval_id(approve_response)
        approve_tool = approval_tool_name(approve_response)
        log("")
        log(f"AGENT < {approve_status or '(none)'}")
        if approve_tool:
            log(f"    tool: {approve_tool}")
        if approve_approval:
            log(f"    approval: {approve_approval}")
        approve_text = user_text_from_response(approve_response)
        if approve_text:
            log_block(approve_text)
        if artifact := artifact_from_response(approve_response):
            uri = artifact.get("uri") if isinstance(artifact, dict) else ""
            label = artifact.get("label") if isinstance(artifact, dict) else ""
            log(f"[artifact] {label or artifact.get('kind', 'artifact')}: {uri or artifact.get('id', '')}")

    passed, reason = step_passed(step, current)
    result["passed"] = passed
    final_approval = approval_id(current.get("response"))
    if final_approval:
        result["lastApprovalId"] = final_approval
    if reason:
        result["failureReason"] = reason
        log("")
        log(f"FAIL {reason}")
    else:
        log("")
        log("OK")
    return result


def resolve_usecase_paths(target: Path) -> list[Path]:
    path = target.resolve()
    if path.is_dir():
        return sorted(p for p in path.glob("*.json") if p.is_file())
    return [path]


def write_summary(artifact_dir: Path, summaries: list[dict[str, Any]], passed: bool) -> Path:
    path = artifact_dir.resolve() / DEFAULT_SUMMARY_FILE
    write_json(path, {
        "passed": passed,
        "finishedAt": utc_now(),
        "usecasesRun": summaries,
    })
    return path


def run_one_usecase(args: argparse.Namespace, usecase_path: Path) -> tuple[int, dict[str, Any]]:
    usecase = load_json(usecase_path)
    run_id = "uc_" + dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ") + "_" + uuid.uuid4().hex[:8]
    session_prefix = str(usecase.get("sessionPrefix") or usecase.get("name") or "eval_agent").strip()
    session_id = args.session.strip() or f"{session_prefix}_{run_id}"
    channel = args.channel.strip() or str(usecase.get("channel") or "eval-cmd").strip()

    variables = {
        "RUN_ID": run_id,
        "SESSION_ID": session_id,
        "CHANNEL": channel,
    }
    if args.dry_run:
        variables["LAST_APPROVAL_ID"] = "dry_run_approval"
    usecase_variables = usecase.get("variables")
    if isinstance(usecase_variables, dict):
        for key, value in usecase_variables.items():
            variables[str(key)] = str(value)

    required_env = usecase.get("requiredEnv", ["OPENAI_API_KEY"])
    missing = [] if args.dry_run else require_env([str(x) for x in required_env])
    artifact_name = safe_filename(str(usecase.get("name") or usecase_path.stem))
    artifact_path = args.artifact_dir.resolve() / f"{artifact_name}.result.json"
    log("")
    log("=" * 72)
    log(f"Use case : {usecase.get('name') or usecase_path.stem}")
    log(f"Source   : {display_path(usecase_path)}")
    log(f"Session  : {session_id}")
    log("=" * 72)
    report: dict[str, Any] = {
        "runId": run_id,
        "usecase": usecase.get("name") or usecase_path.stem,
        "sessionId": session_id,
        "startedAt": utc_now(),
        "finishedAt": "",
        "conversation": [],
        "passed": False,
    }
    if args.dry_run:
        report["dryRun"] = True
    if missing:
        report["missingEnv"] = missing

    write_json(artifact_path, report)
    if missing:
        report["finishedAt"] = utc_now()
        report["failureReason"] = "missing required env: " + ", ".join(missing)
        write_json(artifact_path, report)
        log(f"RUN FAIL: {report['failureReason']}")
        log(f"Artifact: {display_path(artifact_path)}")
        return 2, {
            "passed": False,
            "artifactPath": str(artifact_path),
            "failureReason": report["failureReason"],
            "usecase": str(usecase_path),
        }

    try:
        executable_index = 0
        for step in usecase.get("steps", []):
            if not should_execute_step(step):
                continue
            executable_index += 1
            step_variables = dict(variables)
            step_variables["STEP_NUMBER"] = str(executable_index)
            step_result = run_step(
                step=step,
                usecase=usecase,
                variables=step_variables,
                session_id=session_id,
                channel=channel,
                timeout_seconds=args.timeout_seconds,
                dry_run=args.dry_run,
            )
            for message in step_result.get("messages", []):
                if not isinstance(message, dict):
                    continue
                user_text = str(message.get("user") or "").strip()
                if user_text:
                    report["conversation"].append({
                        "step": step_result.get("id"),
                        "role": "user",
                        "text": user_text,
                    })
                agent_text = str(message.get("agent") or "").strip()
                if agent_text:
                    report["conversation"].append({
                        "step": step_result.get("id"),
                        "role": "agent",
                        "status": message.get("status"),
                        "approvalId": message.get("approvalId"),
                        "approvalTool": message.get("approvalTool"),
                        "artifact": message.get("artifact"),
                        "text": agent_text,
                    })
            latest_approval = str(step_result.get("lastApprovalId") or "").strip()
            if latest_approval:
                variables["LAST_APPROVAL_ID"] = latest_approval
            write_json(artifact_path, report)
            if not step_result.get("passed"):
                report["finishedAt"] = utc_now()
                report["failureReason"] = f"step failed: {step_result.get('id')}"
                report["failedStep"] = step_result.get("id")
                write_json(artifact_path, report)
                log(f"RUN FAIL: {report['failureReason']}")
                log(f"Artifact: {display_path(artifact_path)}")
                return 1, {
                    "passed": False,
                    "artifactPath": str(artifact_path),
                    "failedStep": step_result.get("id"),
                    "failureReason": step_result.get("failureReason"),
                    "usecase": str(usecase_path),
                }
    except Exception as exc:
        report["finishedAt"] = utc_now()
        report["failureReason"] = str(exc)
        write_json(artifact_path, report)
        log(f"RUN FAIL: {report['failureReason']}")
        log(f"Artifact: {display_path(artifact_path)}")
        return 1, {
            "passed": False,
            "artifactPath": str(artifact_path),
            "failureReason": str(exc),
            "usecase": str(usecase_path),
        }

    report["finishedAt"] = utc_now()
    report["passed"] = True
    write_json(artifact_path, report)
    log("RUN OK")
    log(f"Artifact: {display_path(artifact_path)}")
    return 0, {
        "passed": True,
        "artifactPath": str(artifact_path),
        "sessionId": session_id,
        "runId": run_id,
        "usecase": str(usecase_path),
    }


def main() -> int:
    parser = argparse.ArgumentParser(description="Run real V-Claw agent use cases through cmd/vclaw.")
    parser.add_argument("--usecase", type=Path, default=DEFAULT_USECASE, help="Optional use case JSON file or directory. Defaults to testing-e2e/usecases.")
    parser.add_argument("--artifact-dir", type=Path, default=DEFAULT_ARTIFACT_DIR)
    parser.add_argument("--session", default="")
    parser.add_argument("--channel", default="")
    parser.add_argument("--timeout-seconds", type=int, default=300)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    loaded_env = load_default_env_files()
    usecase_paths = resolve_usecase_paths(args.usecase)
    log(f"Found {len(usecase_paths)} use case(s)")
    if not usecase_paths:
        log(f"RUN FAIL: no usecase JSON files found in {args.usecase}")
        return 2

    summaries: list[dict[str, Any]] = []
    for usecase_path in usecase_paths:
        args.loaded_env = loaded_env
        code, summary = run_one_usecase(args, usecase_path)
        summaries.append(summary)
        if code != 0:
            summary_path = write_summary(args.artifact_dir, summaries, False)
            log("")
            log(f"SUMMARY FAIL ({len(summaries)}/{len(usecase_paths)} run)")
            log(f"Summary: {display_path(summary_path)}")
            return code

    summary_path = write_summary(args.artifact_dir, summaries, True)
    log("")
    log(f"SUMMARY OK ({len(summaries)}/{len(usecase_paths)} run)")
    log(f"Summary: {display_path(summary_path)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
