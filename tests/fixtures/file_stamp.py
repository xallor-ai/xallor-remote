"""Observe atomic state replacement without opening the target for reading."""
import ctypes
import os


def stamp(path):
    if os.name != "nt":
        return path.stat().st_mtime_ns
    from ctypes import wintypes

    class Attributes(ctypes.Structure):
        _fields_ = [("attributes", wintypes.DWORD),
                    ("created", wintypes.FILETIME), ("accessed", wintypes.FILETIME),
                    ("modified", wintypes.FILETIME),
                    ("size_high", wintypes.DWORD), ("size_low", wintypes.DWORD)]

    api = ctypes.WinDLL("kernel32", use_last_error=True)
    api.GetFileAttributesExW.argtypes = [wintypes.LPCWSTR, ctypes.c_int, ctypes.c_void_p]
    api.GetFileAttributesExW.restype = wintypes.BOOL
    attributes = Attributes()
    if not api.GetFileAttributesExW(str(path), 0, ctypes.byref(attributes)):
        raise ctypes.WinError(ctypes.get_last_error())
    return attributes.modified.dwHighDateTime << 32 | attributes.modified.dwLowDateTime
