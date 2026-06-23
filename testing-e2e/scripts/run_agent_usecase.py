#!/usr/bin/env python3

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
DEFAULT_ARTIFACT_DIR = REPO_ROOT / "testing-e2e" / "artifacts" / "usecases"
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


def load_json(path: Path) -> Any:
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


def approval_tool_ref(response: dict[str, Any] | None) -> dict[str, str]:
    if not response:
        return {}
    request = response.get("approvalRequest")
    if not isinstance(request, dict):
        return {}
    tool_call = request.get("toolCall")
    if not isinstance(tool_call, dict):
        return {}
    ref = {
        "toolCallId": str(tool_call.get("toolCallId") or "").strip(),
        "toolName": str(tool_call.get("toolName") or "").strip(),
    }
    return {key: value for key, value in ref.items() if value}


def pending_approval_tool_refs(variables: dict[str, str]) -> list[dict[str, str]]:
    ref = {
        "toolCallId": str(variables.get("LAST_APPROVAL_TOOL_CALL_ID") or "").strip(),
        "toolName": str(variables.get("LAST_APPROVAL_TOOL_NAME") or "").strip(),
    }
    ref = {key: value for key, value in ref.items() if value}
    return [ref] if ref else []


def same_tool_call(item: dict[str, Any], ref: dict[str, str]) -> bool:
    item_call_id = str(item.get("toolCallId") or "").strip()
    ref_call_id = str(ref.get("toolCallId") or "").strip()
    if item_call_id and ref_call_id:
        return item_call_id == ref_call_id

    item_name = str(item.get("toolName") or "").strip()
    ref_name = str(ref.get("toolName") or "").strip()
    return bool(item_name and ref_name and item_name == ref_name)


def tool_trace_from_response(response: dict[str, Any] | None) -> list[dict[str, Any]]:
    if not response:
        return []

    trace: list[dict[str, Any]] = []
    request = response.get("approvalRequest")
    if isinstance(request, dict):
        tool_call = request.get("toolCall")
        if isinstance(tool_call, dict):
            item: dict[str, Any] = {
                "phase": "approval_requested",
                "toolName": str(tool_call.get("toolName") or "").strip(),
                "toolCallId": str(tool_call.get("toolCallId") or "").strip(),
            }
            if request.get("approvalId"):
                item["approvalId"] = request.get("approvalId")
            if request.get("riskLevel"):
                item["riskLevel"] = request.get("riskLevel")
            if isinstance(tool_call.get("input"), dict):
                item["input"] = tool_call["input"]
            trace.append({key: value for key, value in item.items() if value not in ("", None)})

    for result in response.get("toolResults") or []:
        if not isinstance(result, dict):
            continue
        item = {
            "phase": "completed",
            "toolName": str(result.get("toolName") or "").strip(),
            "toolCallId": str(result.get("toolCallId") or "").strip(),
            "success": result.get("success"),
        }
        if result.get("data") is not None:
            item["data"] = result.get("data")
        if result.get("error") is not None:
            item["error"] = result.get("error")
        if result.get("artifactRef") is not None:
            item["artifactRef"] = result.get("artifactRef")
        if result.get("metadata") is not None:
            item["metadata"] = result.get("metadata")
        trace.append({key: value for key, value in item.items() if value not in ("", None)})

    return trace


def tool_names_from_trace(
    trace: list[dict[str, Any]],
    exclude_completed_tool_calls: list[dict[str, str]] | None = None,
) -> list[str]:
    names: list[str] = []
    seen: set[str] = set()
    exclude_completed_tool_calls = exclude_completed_tool_calls or []
    for item in trace:
        if item.get("phase") == "completed" and any(
            same_tool_call(item, ref) for ref in exclude_completed_tool_calls
        ):
            continue
        name = str(item.get("toolName") or "").strip()
        if not name or name in seen:
            continue
        seen.add(name)
        names.append(name)
    return names


def response_trace_data(response: dict[str, Any] | None) -> dict[str, Any]:
    if not isinstance(response, dict) or not isinstance(response.get("data"), dict):
        return {}
    data = dict(response["data"])
    data.pop("toolsExposed", None)
    return data


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


def summarize_run(
    run: dict[str, Any],
    user_message: str,
    exclude_completed_tool_calls: list[dict[str, str]] | None = None,
) -> dict[str, Any]:
    response = run.get("response")
    tool_trace = tool_trace_from_response(response)
    summary: dict[str, Any] = {
        "user": user_message,
        "status": response_status(response),
        "agent": user_text_from_response(response),
        "durationMs": run.get("durationMs"),
        "exitCode": run.get("exitCode"),
    }
    if tool_trace:
        tools = tool_names_from_trace(tool_trace, exclude_completed_tool_calls)
        if tools:
            summary["tools"] = tools
        summary["toolTrace"] = tool_trace
    if trace := response_trace_data(response):
        summary["trace"] = trace
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


def str_list(value: Any) -> list[str]:
    if value is None:
        return []
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    text = str(value).strip()
    return [text] if text else []


def agent_expectations_for_step(step: Any) -> dict[str, Any]:
    if not isinstance(step, dict):
        return {}
    agent = step.get("agent")
    if isinstance(agent, dict):
        return dict(agent)
    return {}


def check_agent_expectations(step: dict[str, Any], final_run: dict[str, Any]) -> tuple[bool, str]:
    agent = agent_expectations_for_step(step)
    if not agent:
        return True, ""

    response = final_run.get("response")
    summary = final_run.get("summary")
    if not isinstance(summary, dict):
        summary = {}
    status = response_status(response)
    supported = {
        "expectation",
        "requires_approval",
        "expected_tools",
        "expected_approval_tool",
        "expected_status",
        "response_contains",
    }
    for key in agent:
        if key not in supported:
            return False, f"unsupported agent expectation key {key!r}"

    if "requires_approval" in agent:
        requires_approval = bool(agent["requires_approval"])
        approval_required = status == "approval_required"
        if requires_approval and not approval_required:
            return False, f"expected approval_required status, got {status!r}"
        if not requires_approval and approval_required:
            return False, "expected no approval request, got approval_required"

    expected_status = str(agent.get("expected_status") or "").strip()
    if expected_status and status != expected_status:
        return False, f"expected status {expected_status!r}, got {status!r}"

    if "expected_tools" in agent:
        expected_tools = str_list(agent.get("expected_tools"))
        observed_tools = str_list(summary.get("tools"))
        missing_tools = [tool for tool in expected_tools if tool not in observed_tools]
        if missing_tools:
            return False, f"missing expected tools {missing_tools!r}, got {observed_tools!r}"

    if "expected_approval_tool" in agent:
        expected_approval_tool = agent.get("expected_approval_tool")
        expected_approval_tool = "" if expected_approval_tool is None else str(expected_approval_tool).strip()
        observed_approval_tool = str(summary.get("approvalTool") or "").strip()
        if observed_approval_tool != expected_approval_tool:
            return False, f"expected approvalTool {expected_approval_tool!r}, got {observed_approval_tool!r}"

    response_contains = str_list(agent.get("response_contains"))
    if response_contains:
        agent_text = str(summary.get("agent") or "")
        missing = [text for text in response_contains if text not in agent_text]
        if missing:
            return False, f"response missing expected text {missing!r}"

    return True, ""


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

    agent_ok, agent_reason = check_agent_expectations(step, final_run)
    if not agent_ok:
        return False, agent_reason

    return True, ""


def normalize_step(step: dict[str, Any], variables: dict[str, str]) -> dict[str, Any]:
    step_number = variables.get("STEP_NUMBER", "0")
    if not isinstance(step, dict):
        raise ValueError(f"step {step_number} must be an object")

    normalized = dict(step)
    if "step" in normalized and "id" not in normalized:
        normalized["id"] = str(normalized["step"])
    normalized.setdefault("id", f"step_{step_number}")
    normalized.setdefault("name", f"Step {normalized['id']}")

    user = normalized.get("user")
    if not isinstance(user, dict):
        raise ValueError(f"step {normalized['id']!r} must contain user.message")
    message = str(user.get("message") or "").strip()
    if not message:
        raise ValueError(f"step {normalized['id']!r} has empty user.message")
    normalized["prompt"] = message
    return normalized


def run_step(
    step: dict[str, Any],
    usecase: dict[str, Any],
    variables: dict[str, str],
    session_id: str,
    channel: str,
    timeout_seconds: int,
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
    agent_expectations = agent_expectations_for_step(step)
    if agent_expectations:
        result["agent"] = agent_expectations

    command = build_agent_command(prompt, session_id, channel, usecase, timeout_seconds)
    log("")
    log(step_separator(result["id"]))
    log("USER >")
    log_block(prompt)
    agent_expectation = str(agent_expectations.get("expectation") or "").strip()
    if agent_expectation:
        log("")
        log("EXPECTED AGENT >")
        log_block(agent_expectation)
    first_run = run_agent_command(command, timeout_seconds)
    first_summary = summarize_run(
        first_run,
        prompt,
        pending_approval_tool_refs(variables),
    )
    first_run["summary"] = first_summary
    result["messages"].append(first_summary)
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

    current = first_run
    passed, reason = step_passed(step, current)
    result["passed"] = passed
    final_approval = approval_id(current.get("response"))
    if final_approval:
        result["lastApprovalId"] = final_approval
        if final_tool_ref := approval_tool_ref(current.get("response")):
            if tool_call_id := final_tool_ref.get("toolCallId"):
                result["lastApprovalToolCallId"] = tool_call_id
            if tool_name := final_tool_ref.get("toolName"):
                result["lastApprovalToolName"] = tool_name
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


def usecase_steps_and_config(raw_usecase: Any, usecase_path: Path) -> tuple[list[Any], dict[str, Any]]:
    if isinstance(raw_usecase, list):
        return raw_usecase, {}
    if isinstance(raw_usecase, dict):
        steps = raw_usecase.get("steps", [])
        if not isinstance(steps, list):
            raise ValueError(f"{usecase_path.name} steps must be a list")
        return steps, raw_usecase
    raise ValueError(f"{usecase_path.name} must be a list or object")


def run_one_usecase(args: argparse.Namespace, usecase_path: Path) -> tuple[int, dict[str, Any]]:
    raw_usecase = load_json(usecase_path)
    steps, usecase = usecase_steps_and_config(raw_usecase, usecase_path)
    usecase_name = usecase_path.stem
    run_id = "uc_" + dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ") + "_" + uuid.uuid4().hex[:8]
    session_prefix = str(usecase.get("sessionPrefix") or usecase_name).strip()
    session_id = args.session.strip() or f"{session_prefix}_{run_id}"
    channel = args.channel.strip() or str(usecase.get("channel") or "eval-cmd").strip()

    variables = {
        "RUN_ID": run_id,
        "SESSION_ID": session_id,
        "CHANNEL": channel,
    }
    usecase_variables = usecase.get("variables")
    if isinstance(usecase_variables, dict):
        for key, value in usecase_variables.items():
            variables[str(key)] = str(value)

    required_env = usecase.get("requiredEnv", ["OPENAI_API_KEY"])
    missing = require_env([str(x) for x in required_env])
    artifact_path = args.artifact_dir.resolve() / usecase_path.name
    log("")
    log("=" * 72)
    log(f"Use case : {usecase_name}")
    log(f"Source   : {display_path(usecase_path)}")
    log(f"Session  : {session_id}")
    log("=" * 72)
    report: dict[str, Any] = {
        "runId": run_id,
        "usecase": usecase_name,
        "sessionId": session_id,
        "startedAt": utc_now(),
        "finishedAt": "",
        "conversation": [],
        "passed": False,
    }
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
        for step in steps:
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
            )
            for message in step_result.get("messages", []):
                if not isinstance(message, dict):
                    continue
                turn_entry: dict[str, Any] = {
                    "step": int(step_result.get("id")),
                    "passed": bool(step_result.get("passed")),
                }
                if failure_reason := str(step_result.get("failureReason") or "").strip():
                    turn_entry["failureReason"] = failure_reason
                user_text = str(message.get("user") or "").strip()
                if user_text:
                    turn_entry["user"] = {
                        "message": user_text,
                    }

                agent_payload: dict[str, Any] = {}
                agent_text = str(message.get("agent") or "").strip()
                if agent_text:
                    agent_payload["message"] = agent_text
                for key in ("status", "approvalId", "approvalTool", "artifact", "tools", "toolTrace", "trace"):
                    if key in message and message[key] not in (None, "", [], {}):
                        agent_payload[key] = message[key]
                if agent_payload:
                    turn_entry["agent"] = agent_payload

                if "user" in turn_entry or "agent" in turn_entry:
                    report["conversation"].append(turn_entry)
            latest_approval = str(step_result.get("lastApprovalId") or "").strip()
            if latest_approval:
                variables["LAST_APPROVAL_ID"] = latest_approval
                variables["LAST_APPROVAL_TOOL_CALL_ID"] = str(
                    step_result.get("lastApprovalToolCallId") or ""
                ).strip()
                variables["LAST_APPROVAL_TOOL_NAME"] = str(
                    step_result.get("lastApprovalToolName") or ""
                ).strip()
            else:
                variables.pop("LAST_APPROVAL_ID", None)
                variables.pop("LAST_APPROVAL_TOOL_CALL_ID", None)
                variables.pop("LAST_APPROVAL_TOOL_NAME", None)
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
    args = parser.parse_args()

    load_default_env_files()
    usecase_paths = resolve_usecase_paths(args.usecase)
    log(f"Found {len(usecase_paths)} use case(s)")
    if not usecase_paths:
        log(f"RUN FAIL: no usecase JSON files found in {args.usecase}")
        return 2

    runs_completed = 0
    for usecase_path in usecase_paths:
        code, _summary = run_one_usecase(args, usecase_path)
        runs_completed += 1
        if code != 0:
            log("")
            log(f"RUNS FAIL ({runs_completed}/{len(usecase_paths)} run)")
            return code

    log("")
    log(f"RUNS OK ({runs_completed}/{len(usecase_paths)} run)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
