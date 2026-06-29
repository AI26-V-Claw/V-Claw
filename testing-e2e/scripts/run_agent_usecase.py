#!/usr/bin/env python3

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import mimetypes
import os
import re
import shutil
import subprocess
import sys
import time
import uuid
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_USECASE = REPO_ROOT / "testing-e2e" / "usecases"
DEFAULT_ARTIFACT_DIR = REPO_ROOT / "testing-e2e" / "artifacts" / "usecases"
DEFAULT_SESSION_SEEDS = REPO_ROOT / "testing-e2e" / "sessions"
DEFAULT_SANDBOX_WORKSPACE_DIR = REPO_ROOT / ".sandbox-workspace"
E2E_ATTACHMENT_DIR = Path("data") / "e2e_attachments"
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


def resolve_repo_path(path_text: str) -> Path:
    path = Path(path_text)
    if not path.is_absolute():
        path = REPO_ROOT / path
    return path


def sandbox_workspace_root() -> Path:
    workspace_dir = os.environ.get("VCLAW_SANDBOX_WORKSPACE_DIR", "").strip()
    root = resolve_repo_path(workspace_dir) if workspace_dir else DEFAULT_SANDBOX_WORKSPACE_DIR
    return root / "agent" / "workspace"


def sanitize_path_component(value: str) -> str:
    cleaned = []
    for char in value.strip():
        if char.isascii() and (char.isalnum() or char in ("-", "_", ".")):
            cleaned.append(char)
        else:
            cleaned.append("_")
    result = "".join(cleaned).strip("._")
    return result or "unnamed"


def sanitize_session_id(session_id: str) -> str:
    changed = False
    parts: list[str] = []
    for char in session_id:
        if char.isascii() and (char.isalnum() or char in ("-", "_")):
            parts.append(char)
        else:
            parts.append("_")
            changed = True
    result = "".join(parts)
    if not result:
        return "_empty"
    if changed:
        digest = hashlib.sha256(session_id.encode("utf-8")).hexdigest()[:12]
        return f"{result}-{digest}"
    return result


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
    status = response_status(response)
    agent_text = user_text_from_response(response)
    if not agent_text:
        stderr = str(run.get("stderr") or "").strip()
        stdout = str(run.get("stdout") or "").strip()
        fallback_text = stderr or stdout
        if fallback_text:
            agent_text = fallback_text[-2000:]
    if not status and run.get("exitCode") not in (0, None):
        status = "failed"
    summary: dict[str, Any] = {
        "user": user_message,
        "status": status,
        "agent": agent_text,
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
            command.extend(["-iteration-budget", value])

    return command


def agent_data_dir(usecase: dict[str, Any]) -> Path:
    data_dir = ""
    flags = usecase.get("agentFlags")
    if isinstance(flags, dict):
        data_dir = str(flags.get("dataDir") or "").strip()
    if not data_dir:
        data_dir = os.environ.get("DATA_DIR", "./data").strip() or "./data"
    return resolve_repo_path(data_dir)


def prepare_seed_session(
    usecase_path: Path,
    usecase: dict[str, Any],
    session_id: str,
) -> dict[str, str] | None:
    source_dir = DEFAULT_SESSION_SEEDS / usecase_path.stem
    if not source_dir.is_dir():
        return None

    sessions_dir = agent_data_dir(usecase) / "sessions"
    destination = sessions_dir / sanitize_session_id(session_id)
    while destination.exists():
        session_id = f"{session_id}_{uuid.uuid4().hex[:8]}"
        destination = sessions_dir / sanitize_session_id(session_id)
    sessions_dir.mkdir(parents=True, exist_ok=True)
    shutil.copytree(source_dir, destination)
    return {
        "sessionId": session_id,
        "source": str(source_dir),
        "destination": str(destination),
    }


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


def attachment_values_from_step(step: dict[str, Any]) -> list[Any]:
    user = step.get("user")
    if not isinstance(user, dict):
        return []
    raw = user.get("attachments")
    if raw is None:
        raw = user.get("files")
    if raw is None:
        return []
    if isinstance(raw, list):
        return raw
    return [raw]


def resolve_attachment_source(path_text: str, usecase_path: Path) -> Path:
    expanded = Path(path_text)
    if expanded.is_absolute():
        return expanded

    usecase_relative_path = usecase_path.parent / path_text
    if usecase_relative_path.exists():
        return usecase_relative_path

    return REPO_ROOT / path_text


def path_is_within(path: Path, parent: Path) -> bool:
    try:
        path.resolve().relative_to(parent.resolve())
        return True
    except ValueError:
        return False


def unique_attachment_destination(destination_dir: Path, filename: str, used: set[str]) -> Path:
    filename = sanitize_path_component(filename)
    stem = Path(filename).stem or "attachment"
    suffix = Path(filename).suffix
    candidate = filename
    counter = 2
    while candidate.lower() in used:
        candidate = f"{stem}_{counter}{suffix}"
        counter += 1
    used.add(candidate.lower())
    return destination_dir / candidate


def prepare_step_attachments(
    step: dict[str, Any],
    variables: dict[str, str],
    usecase_path: Path,
    used_names: set[str],
) -> list[dict[str, Any]]:
    specs = attachment_values_from_step(step)
    if not specs:
        return []

    usecase_name = sanitize_path_component(usecase_path.stem)
    workspace_root = sandbox_workspace_root().resolve()
    destination_dir = workspace_root / E2E_ATTACHMENT_DIR / usecase_name

    attachments: list[dict[str, Any]] = []
    for index, spec in enumerate(specs, start=1):
        source_text = ""
        filename = ""
        mime_type = ""
        if isinstance(spec, str):
            source_text = spec
        elif isinstance(spec, dict):
            source_text = str(spec.get("path") or spec.get("source") or spec.get("localPath") or "").strip()
            filename = str(spec.get("filename") or spec.get("name") or "").strip()
            mime_type = str(spec.get("mimeType") or spec.get("mime") or "").strip()
        else:
            raise ValueError(f"attachment #{index} must be a string or object")

        source_text = expand_vars(source_text, variables).strip()
        if not source_text:
            raise ValueError(f"attachment #{index} is missing path")
        source_path = resolve_attachment_source(source_text, usecase_path).resolve()
        if not source_path.exists():
            raise ValueError(f"attachment not found: {source_text}")
        if not source_path.is_file():
            raise ValueError(f"attachment must be a file: {source_text}")

        filename = expand_vars(filename, variables).strip() if filename else source_path.name
        if not mime_type:
            mime_type = mimetypes.guess_type(filename)[0] or "application/octet-stream"

        if path_is_within(source_path, workspace_root):
            agent_path = source_path
        else:
            destination_dir.mkdir(parents=True, exist_ok=True)
            agent_path = unique_attachment_destination(destination_dir, filename, used_names)
            shutil.copy2(source_path, agent_path)

        attachments.append({
            "path": str(agent_path.resolve()),
            "filename": agent_path.name,
            "mimeType": mime_type,
            "source": display_path(source_path),
        })

    return attachments


def prompt_with_attachment_context(prompt: str, attachments: list[dict[str, Any]]) -> str:
    paths = [str(item.get("path") or "").strip() for item in attachments if str(item.get("path") or "").strip()]
    if not paths:
        return prompt
    lines = [
        "Current user attachments are available as local files.",
        'If the user says "file này", "ảnh này", or asks to send/upload the attached file, use these paths in tool inputs that accept attachments.',
        "Tool mapping: gmail.createDraft.attachments for Gmail, drive.uploadFile.localPath for Drive uploads.",
        "For Google Docs or Google Sheets, first read or parse the local file, then create/update the Docs or Sheets content from that extracted text or table.",
        "Attachment paths:",
    ]
    lines.extend(f"- {path}" for path in paths)
    return prompt.rstrip() + "\n\n" + "\n".join(lines)


def attachment_prompt_variables(attachments: list[dict[str, Any]]) -> dict[str, str]:
    variables: dict[str, str] = {
        "ATTACHMENT_COUNT": str(len(attachments)),
    }
    for index, attachment in enumerate(attachments, start=1):
        path = str(attachment.get("path") or "").strip()
        filename = str(attachment.get("filename") or "").strip()
        source = str(attachment.get("source") or "").strip()
        if path:
            variables[f"ATTACHMENT_{index}_PATH"] = path
            variables["LAST_ATTACHMENT_PATH"] = path
        if filename:
            variables[f"ATTACHMENT_{index}_FILENAME"] = filename
            variables["LAST_ATTACHMENT_FILENAME"] = filename
        if source:
            variables[f"ATTACHMENT_{index}_SOURCE"] = source
            variables["LAST_ATTACHMENT_SOURCE"] = source
    return variables


def str_list(value: Any) -> list[str]:
    if value is None:
        return []
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    text = str(value).strip()
    return [text] if text else []


def int_list(value: Any) -> list[int]:
    if value is None:
        return []
    values = value if isinstance(value, list) else [value]
    result: list[int] = []
    for item in values:
        try:
            result.append(int(item))
        except (TypeError, ValueError):
            continue
    return result


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
    status = response_status(response) or str(summary.get("status") or "").strip()
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
    allowed_exit_codes = int_list(step.get("allowed_exit_codes"))
    if final_run.get("timedOut"):
        return False, "agent command timed out"
    if final_run.get("parseError") and not allowed_exit_codes:
        return False, str(final_run["parseError"])
    if final_run.get("parseError") and allowed_exit_codes:
        stderr = str(final_run.get("stderr") or "").strip()
        stdout = str(final_run.get("stdout") or "").strip()
        if not stderr and not stdout:
            return False, str(final_run["parseError"])
    exit_code = final_run.get("exitCode")
    if allowed_exit_codes:
        if exit_code is None:
            if 0 not in allowed_exit_codes:
                return False, f"agent command exited with {exit_code}, expected one of {allowed_exit_codes!r}"
        elif exit_code not in allowed_exit_codes:
            return False, f"agent command exited with {exit_code}, expected one of {allowed_exit_codes!r}"
    elif exit_code not in (0, None):
        return False, f"agent command exited with {exit_code}"

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
        raise ValueError(f"step {normalized['id']!r} must contain user.message or user.attachments")
    message = str(user.get("message") or "").strip()
    attachments = attachment_values_from_step(normalized)
    if message and attachments:
        raise ValueError(
            f"step {normalized['id']!r} cannot combine user.message and user.attachments; split them into separate steps"
        )
    if not message and not attachments:
        raise ValueError(f"step {normalized['id']!r} must contain user.message or user.attachments")
    normalized["prompt"] = message or "User sent an attachment."
    return normalized


def run_step(
    step: dict[str, Any],
    usecase: dict[str, Any],
    usecase_path: Path,
    variables: dict[str, str],
    attachment_used_names: set[str],
    session_id: str,
    channel: str,
    timeout_seconds: int,
) -> dict[str, Any]:
    step = normalize_step(step, variables)

    attachments = prepare_step_attachments(step, variables, usecase_path, attachment_used_names)
    prompt_variables = dict(variables)
    prompt_variables.update(attachment_prompt_variables(attachments))
    user_prompt = expand_vars(str(step.get("prompt") or ""), prompt_variables).strip()
    if not user_prompt:
        raise ValueError(f"step {step.get('id')!r} has empty prompt")
    prompt = prompt_with_attachment_context(user_prompt, attachments)

    result: dict[str, Any] = {
        "id": step.get("id"),
        "name": step.get("name"),
        "prompt": user_prompt,
        "messages": [],
    }
    if attachments:
        result["attachmentVariables"] = attachment_prompt_variables(attachments)
    if attachments:
        result["attachments"] = attachments
        result["agentPrompt"] = prompt
    agent_expectations = agent_expectations_for_step(step)
    if agent_expectations:
        result["agent"] = agent_expectations

    command = build_agent_command(prompt, session_id, channel, usecase, timeout_seconds)
    log("")
    log(step_separator(result["id"]))
    log("USER >")
    log_block(user_prompt)
    if attachments:
        log("")
        log("ATTACHMENTS >")
        for attachment in attachments:
            log_block(f"{attachment.get('filename')}: {attachment.get('path')}")
    agent_expectation = str(agent_expectations.get("expectation") or "").strip()
    if agent_expectation:
        log("")
        log("EXPECTED AGENT >")
        log_block(agent_expectation)
    first_run = run_agent_command(command, timeout_seconds)
    first_summary = summarize_run(
        first_run,
        user_prompt,
        pending_approval_tool_refs(variables),
    )
    if attachments:
        first_summary["attachments"] = attachments
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


def existing_artifact_report(path: Path) -> dict[str, Any] | None:
    if not path.exists() or not path.is_file():
        return None
    try:
        data = load_json(path)
    except (OSError, json.JSONDecodeError, ValueError):
        return None
    return data if isinstance(data, dict) else None


def run_one_usecase(args: argparse.Namespace, usecase_path: Path) -> tuple[int, dict[str, Any]]:
    raw_usecase = load_json(usecase_path)
    steps, usecase = usecase_steps_and_config(raw_usecase, usecase_path)
    usecase_name = usecase_path.stem
    artifact_path = args.artifact_dir.resolve() / usecase_path.name
    existing_report = existing_artifact_report(artifact_path)
    if existing_report:
        if args.skip_existing:
            log("")
            log("=" * 72)
            log(f"Use case : {usecase_name}")
            log(f"Source   : {display_path(usecase_path)}")
            log(f"Skip     : existing artifact {display_path(artifact_path)}")
            log("=" * 72)
            log("RUN SKIP")
            log(f"Artifact: {display_path(artifact_path)}")
            return 0, {
                "passed": bool(existing_report.get("passed")),
                "artifactPath": str(artifact_path),
                "skipped": True,
                "usecase": str(usecase_path),
            }
        if args.skip_passed and existing_report.get("passed") is True:
            log("")
            log("=" * 72)
            log(f"Use case : {usecase_name}")
            log(f"Source   : {display_path(usecase_path)}")
            log(f"Skip     : existing passed artifact {display_path(artifact_path)}")
            log("=" * 72)
            log("RUN SKIP")
            log(f"Artifact: {display_path(artifact_path)}")
            return 0, {
                "passed": True,
                "artifactPath": str(artifact_path),
                "skipped": True,
                "usecase": str(usecase_path),
            }

    run_id = "uc_" + dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ") + "_" + uuid.uuid4().hex[:8]
    session_prefix = str(usecase.get("sessionPrefix") or usecase_name).strip()
    session_id = args.session.strip() or f"{session_prefix}_{run_id}"
    channel = args.channel.strip() or str(usecase.get("channel") or "eval-cmd").strip()

    required_env = usecase.get("requiredEnv", ["OPENAI_API_KEY"])
    missing = require_env([str(x) for x in required_env])
    seed_session: dict[str, str] | None = None
    if not missing and not args.session.strip():
        seed_session = prepare_seed_session(usecase_path, usecase, session_id)
        if seed_session:
            session_id = seed_session["sessionId"]

    variables = {
        "RUN_ID": run_id,
        "SESSION_ID": session_id,
        "CHANNEL": channel,
    }
    usecase_variables = usecase.get("variables")
    if isinstance(usecase_variables, dict):
        for key, value in usecase_variables.items():
            variables[str(key)] = str(value)

    log("")
    log("=" * 72)
    log(f"Use case : {usecase_name}")
    log(f"Source   : {display_path(usecase_path)}")
    log(f"Session  : {session_id}")
    if seed_session:
        log(f"Seed     : {display_path(Path(seed_session['source']))} -> {display_path(Path(seed_session['destination']))}")
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
    if seed_session:
        report["seedSession"] = {
            "source": display_path(Path(seed_session["source"])),
            "destination": display_path(Path(seed_session["destination"])),
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
        attachment_used_names: set[str] = set()
        for step in steps:
            executable_index += 1
            step_variables = dict(variables)
            step_variables["STEP_NUMBER"] = str(executable_index)
            step_result = run_step(
                step=step,
                usecase=usecase,
                usecase_path=usecase_path,
                variables=step_variables,
                attachment_used_names=attachment_used_names,
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
                user_payload: dict[str, Any] = {}
                if user_text:
                    user_payload["message"] = user_text
                if message.get("attachments"):
                    user_payload["attachments"] = message["attachments"]
                if user_payload:
                    turn_entry["user"] = user_payload

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
            attachment_variables = step_result.get("attachmentVariables")
            if isinstance(attachment_variables, dict):
                for key, value in attachment_variables.items():
                    variables[str(key)] = str(value)
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
    parser.add_argument("--skip-passed", action="store_true", help="Skip use cases whose artifact already exists and is marked passed.")
    parser.add_argument("--skip-existing", action="store_true", help="Skip use cases whose artifact already exists, regardless of pass/fail status.")
    parser.add_argument("--session", default="")
    parser.add_argument("--channel", default="")
    parser.add_argument("--timeout-seconds", type=int, default=300)
    parser.add_argument("--fail-fast", action="store_true", help="Stop after the first failed use case.")
    args = parser.parse_args()

    load_default_env_files()
    usecase_paths = resolve_usecase_paths(args.usecase)
    log(f"Found {len(usecase_paths)} use case(s)")
    if not usecase_paths:
        log(f"RUN FAIL: no usecase JSON files found in {args.usecase}")
        return 2

    runs_completed = 0
    runs_passed = 0
    runs_skipped = 0
    runs_failed = 0
    first_failure_code = 0
    for usecase_path in usecase_paths:
        try:
            code, summary = run_one_usecase(args, usecase_path)
        except Exception as exc:
            code = 1
            summary = {
                "passed": False,
                "failureReason": str(exc),
                "usecase": str(usecase_path),
            }
            log("")
            log("=" * 72)
            log(f"Use case : {usecase_path.stem}")
            log(f"Source   : {display_path(usecase_path)}")
            log("=" * 72)
            log(f"RUN FAIL: {exc}")
        runs_completed += 1

        if summary.get("skipped"):
            runs_skipped += 1
        elif code == 0 and summary.get("passed") is True:
            runs_passed += 1
        else:
            runs_failed += 1

        if code != 0:
            if first_failure_code == 0:
                first_failure_code = code
            if not args.fail_fast:
                log("")
                log(f"RUN CONTINUE ({runs_completed}/{len(usecase_paths)} run)")
                continue
            log("")
            log(f"RUNS FAIL ({runs_completed}/{len(usecase_paths)} run)")
            return code

    log("")
    if runs_failed:
        log(
            f"RUNS FAIL ({runs_completed}/{len(usecase_paths)} run, "
            f"passed={runs_passed}, skipped={runs_skipped}, failed={runs_failed})"
        )
        return first_failure_code or 1

    log(
        f"RUNS OK ({runs_completed}/{len(usecase_paths)} run, "
        f"passed={runs_passed}, skipped={runs_skipped}, failed={runs_failed})"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
