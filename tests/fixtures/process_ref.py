"""Stable native references to test-owned processes, including exited children."""
import ctypes
import os
import select
import signal


class ProcessRef:
    def __init__(self, pid):
        self.handle = None
        if os.name == "nt":
            self.api = ctypes.WinDLL("kernel32", use_last_error=True)
            self.api.OpenProcess.argtypes = [ctypes.c_uint32, ctypes.c_int, ctypes.c_uint32]
            self.api.OpenProcess.restype = ctypes.c_void_p
            self.api.WaitForSingleObject.argtypes = [ctypes.c_void_p, ctypes.c_uint32]
            self.api.TerminateProcess.argtypes = [ctypes.c_void_p, ctypes.c_uint32]
            self.api.CloseHandle.argtypes = [ctypes.c_void_p]
            self.handle = self.api.OpenProcess(0x100001, 0, pid)
            if not self.handle and ctypes.get_last_error() != 87:
                raise ctypes.WinError(ctypes.get_last_error())
        else:
            try:
                self.handle = os.pidfd_open(pid)
            except ProcessLookupError:
                pass

    def alive(self):
        if self.handle is None or self.handle == 0:
            return False
        if os.name == "nt":
            state = self.api.WaitForSingleObject(self.handle, 0)
            if state not in (0, 258):
                raise ctypes.WinError(ctypes.get_last_error())
            return state == 258
        return not select.select([self.handle], [], [], 0)[0]

    def close(self):
        if self.handle is None or self.handle == 0:
            return
        if os.name == "nt":
            if self.alive():
                if not self.api.TerminateProcess(self.handle, 1):
                    raise ctypes.WinError(ctypes.get_last_error())
                assert self.api.WaitForSingleObject(self.handle, 5000) == 0
            self.api.CloseHandle(self.handle)
        else:
            if self.alive():
                signal.pidfd_send_signal(self.handle, signal.SIGKILL)
                assert select.select([self.handle], [], [], 5)[0]
            os.close(self.handle)
        self.handle = None
