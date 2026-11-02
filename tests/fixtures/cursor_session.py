"""Official CLI session-ID allocation, separate from Agent execution completion."""
import re
import subprocess

UUID_LINE = re.compile(r"\s*([0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})\s*", re.I)


def create_chat(agent, workspace, timeout=45):
    """Return an explicit ID; a timed-out allocator is already killed by run().

    Cursor 2026.10.22 on Windows can print the ID without exiting. This does not
    establish Agent delivery/completion: the caller must independently verify
    matching init/result and context after resume. Never infer an ID from logs.
    """
    exit_observed = True
    try:
        result = subprocess.run([*agent, "--workspace", str(workspace), "create-chat"], cwd=workspace,
                                stdin=subprocess.DEVNULL, capture_output=True, timeout=timeout)
        if result.returncode:
            raise AssertionError("Official Cursor create-chat exited unsuccessfully")
        output = result.stdout
    except subprocess.TimeoutExpired as error:
        exit_observed = False
        output = error.stdout or b""
    text = output.decode("utf-8", errors="replace")
    match = UUID_LINE.fullmatch(text)
    if not match:
        raise AssertionError("Official Cursor create-chat did not return exactly one session UUID")
    return match.group(1).lower(), exit_observed
