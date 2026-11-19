"""Read-only Windows process references and original image mapping checks."""
import ctypes
import os
from pathlib import Path


class WindowsProcessImage:
    def __init__(self, pid):
        if os.name != "nt":
            raise RuntimeError("Windows process observation is unavailable")
        if type(pid) is not int or not 0 < pid <= 0xffffffff:
            raise ValueError("Invalid Runtime PID")
        self.pid = pid
        self.api = ctypes.WinDLL("kernel32", use_last_error=True)
        self.nt = ctypes.WinDLL("ntdll")
        handle = ctypes.c_void_p
        self.api.OpenProcess.argtypes = [ctypes.c_uint32, ctypes.c_int, ctypes.c_uint32]
        self.api.OpenProcess.restype = handle
        self.api.CloseHandle.argtypes = [handle]
        self.api.CloseHandle.restype = ctypes.c_int
        self.api.WaitForSingleObject.argtypes = [handle, ctypes.c_uint32]
        self.api.WaitForSingleObject.restype = ctypes.c_uint32
        self.api.CreateFileW.argtypes = [ctypes.c_wchar_p, ctypes.c_uint32, ctypes.c_uint32,
                                        handle, ctypes.c_uint32, ctypes.c_uint32, handle]
        self.api.CreateFileW.restype = handle
        self.nt.NtQueryInformationProcess.argtypes = [handle, ctypes.c_uint32, handle,
                                                       ctypes.c_uint32, handle]
        self.nt.NtQueryInformationProcess.restype = ctypes.c_long
        # SYNCHRONIZE + QUERY_INFORMATION only. No terminate/write access.
        self.handle = self.api.OpenProcess(0x100400, 0, pid)
        if not self.handle:
            raise ctypes.WinError(ctypes.get_last_error())

    def close(self):
        if self.handle is not None:
            if not self.api.CloseHandle(self.handle):
                raise ctypes.WinError(ctypes.get_last_error())
            self.handle = None

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        self.close()

    def exited(self):
        if self.handle is None:
            raise ValueError("Process observation already closed")
        state = self.api.WaitForSingleObject(self.handle, 0)
        if state == 0:
            return True
        if state == 258:
            return False
        raise ctypes.WinError(ctypes.get_last_error())

    def parent_pid(self):
        from runtime_process_windows import parent_pid
        return parent_pid(self)

    def arguments(self):
        from runtime_process_windows import arguments
        return arguments(self)

    def image_path(self):
        from runtime_process_windows import image_path
        return image_path(self)

    def observe(self, selected, manifest, entry=None, expected_parent=None):
        import msvcrt
        from runtime_image_observation import hash_open_image
        if self.exited():
            raise ProcessLookupError("Runtime exited before image observation")
        selected = Path(selected)
        # Keep this exact file object stable while mapping and hashing it.
        file_handle = self.api.CreateFileW(str(selected), 0x80100020, 1, None, 3, 0x80, None)
        if file_handle == ctypes.c_void_p(-1).value:
            raise ctypes.WinError(ctypes.get_last_error())
        try:
            mapped = ctypes.c_void_p(file_handle)
            # Same ProcessImageFileMapping(44) primitive as runtimeprocess:
            # compare this file HANDLE with the process's original image section.
            status = self.nt.NtQueryInformationProcess(self.handle, 44, ctypes.byref(mapped),
                                                       ctypes.sizeof(mapped), None)
            if status != 0:
                raise ValueError(f"Kernel process image differs from selected file (NTSTATUS {status & 0xffffffff:08x})")
            descriptor = msvcrt.open_osfhandle(file_handle, os.O_RDONLY | os.O_BINARY)
            file_handle = None  # ownership transferred to the CRT descriptor
            try:
                image = os.fdopen(descriptor, "rb")
            except BaseException:
                os.close(descriptor)
                raise
            with image:
                digest = hash_open_image(image, manifest["artifact"])
            parent = self.parent_pid()
            if expected_parent is not None and parent != expected_parent:
                raise ValueError("Process is not a child of the retained parent")
            if entry is not None:
                if entry not in ("runtime-serve", "runtime-transaction"):
                    raise ValueError("Unsupported Runtime observation entry")
                args = self.arguments()
                if (len(args)!=4 or os.path.normcase(str(Path(args[0]).resolve()))!=os.path.normcase(str(selected.resolve()))
                        or args[1:]!=["--require-profile-format",str(manifest["build"]["profile_format"]),entry]):
                    raise ValueError("Process is not the scoped Runtime entry")
            if self.exited():
                raise ProcessLookupError("Runtime exited during image observation")
            result = {"loaded_sha256": digest, "kernel_image_mapping": True,
                      "process_handle_observed": True, "parent_pid": parent}
            if entry is not None:
                result["entry"] = entry
            return result
        finally:
            if file_handle is not None and not self.api.CloseHandle(file_handle):
                raise ctypes.WinError(ctypes.get_last_error())
