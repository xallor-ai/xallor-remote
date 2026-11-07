"""Wait for a complete listener address, not merely a created ready file."""
import time
from urllib.parse import urlsplit


def wait_ready_url(path, process, timeout=15):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        code = process.poll()
        if code is not None:
            raise RuntimeError(f"fixture exited before readiness: {code}")
        try:
            record = path.read_text(encoding="utf-8")
        except OSError:
            # Windows may briefly deny reads while the producer owns the file.
            record = ""
        if record.endswith("\n"):
            url = record.strip()
            address = urlsplit(url)
            if address.scheme != "http" or address.hostname != "127.0.0.1" or not address.port:
                raise ValueError("fixture published an invalid listener address")
            return url
        time.sleep(.05)
    raise TimeoutError("fixture did not publish a complete listener address")
