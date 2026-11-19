"""Read-only Windows process discovery and held-handle role observations.

Toolhelp IDs are hints, never authority. Every accepted process is subsequently
checked through its own handle and original image mapping.
"""
import ctypes
import os


class BasicInformation(ctypes.Structure):
    _fields_=[("exit_status",ctypes.c_long),("peb",ctypes.c_void_p),("affinity",ctypes.c_size_t),
              ("priority",ctypes.c_long),("pid",ctypes.c_size_t),("parent",ctypes.c_size_t)]


class UnicodeString(ctypes.Structure):
    _fields_=[("length",ctypes.c_ushort),("maximum",ctypes.c_ushort),("buffer",ctypes.c_void_p)]


class ProcessEntry(ctypes.Structure):
    _fields_=[("size",ctypes.c_uint32),("usage",ctypes.c_uint32),("pid",ctypes.c_uint32),
              ("heap",ctypes.c_size_t),("module",ctypes.c_uint32),("threads",ctypes.c_uint32),
              ("parent",ctypes.c_uint32),("priority",ctypes.c_long),("flags",ctypes.c_uint32),
              ("name",ctypes.c_wchar*260)]


def child_process_ids(parent_pid):
    if os.name!="nt" or type(parent_pid) is not int or parent_pid<=0:
        raise ValueError("Windows parent process ID required")
    api=ctypes.WinDLL("kernel32",use_last_error=True)
    api.CreateToolhelp32Snapshot.argtypes=[ctypes.c_uint32,ctypes.c_uint32]
    api.CreateToolhelp32Snapshot.restype=ctypes.c_void_p
    for name in ("Process32FirstW","Process32NextW"):
        function=getattr(api,name)
        function.argtypes=[ctypes.c_void_p,ctypes.POINTER(ProcessEntry)]
        function.restype=ctypes.c_int
    api.CloseHandle.argtypes=[ctypes.c_void_p];api.CloseHandle.restype=ctypes.c_int
    snapshot=api.CreateToolhelp32Snapshot(2,0)
    if snapshot==ctypes.c_void_p(-1).value:
        raise ctypes.WinError(ctypes.get_last_error())
    try:
        row=ProcessEntry();row.size=ctypes.sizeof(row)
        found=[];count=0
        exists=api.Process32FirstW(snapshot,ctypes.byref(row))
        while exists:
            count+=1
            if count>100000:raise RuntimeError("Process snapshot exceeds bound")
            if row.parent==parent_pid:found.append(int(row.pid))
            exists=api.Process32NextW(snapshot,ctypes.byref(row))
        if ctypes.get_last_error()!=18:raise ctypes.WinError(ctypes.get_last_error())
        return found
    finally:
        if not api.CloseHandle(snapshot):raise ctypes.WinError(ctypes.get_last_error())


def parent_pid(process):
    if process.exited():raise ProcessLookupError("Process already exited")
    basic=BasicInformation()
    status=process.nt.NtQueryInformationProcess(process.handle,0,ctypes.byref(basic),ctypes.sizeof(basic),None)
    if status!=0 or basic.pid!=process.pid or not basic.parent:
        raise RuntimeError("Native process parent observation failed")
    if process.exited():raise ProcessLookupError("Process exited during parent observation")
    return int(basic.parent)


def image_path(process):
    if process.exited():raise ProcessLookupError("Process already exited")
    function=process.api.QueryFullProcessImageNameW
    function.argtypes=[ctypes.c_void_p,ctypes.c_uint32,ctypes.c_wchar_p,ctypes.POINTER(ctypes.c_uint32)]
    function.restype=ctypes.c_int
    buffer=ctypes.create_unicode_buffer(32768);size=ctypes.c_uint32(len(buffer))
    if not function(process.handle,0,buffer,ctypes.byref(size)):
        raise ctypes.WinError(ctypes.get_last_error())
    if process.exited():raise ProcessLookupError("Process exited during image-path observation")
    return buffer.value


def arguments(process):
    if process.exited():raise ProcessLookupError("Process already exited")
    needed=ctypes.c_uint32()
    process.nt.NtQueryInformationProcess(process.handle,60,None,0,ctypes.byref(needed))
    if not ctypes.sizeof(UnicodeString)<needed.value<=131072:
        raise RuntimeError("Native command-line size is unavailable or exceeds bound")
    buffer=ctypes.create_string_buffer(needed.value)
    returned=ctypes.c_uint32()
    status=process.nt.NtQueryInformationProcess(process.handle,60,buffer,len(buffer),ctypes.byref(returned))
    if status!=0 or not ctypes.sizeof(UnicodeString)<=returned.value<=len(buffer):
        raise RuntimeError("Native command-line observation failed")
    value=UnicodeString.from_buffer(buffer)
    base=ctypes.addressof(buffer);pointer=value.buffer or 0
    if (not value.length or value.length%2 or value.length>value.maximum or pointer<base+ctypes.sizeof(UnicodeString)
            or pointer+value.length>base+returned.value):
        raise ValueError("Native command-line string is outside its returned buffer")
    text=ctypes.string_at(pointer,value.length).decode("utf-16-le")
    if "\0" in text:raise ValueError("Native command-line contains embedded null")
    shell=ctypes.WinDLL("shell32",use_last_error=True)
    shell.CommandLineToArgvW.argtypes=[ctypes.c_wchar_p,ctypes.POINTER(ctypes.c_int)]
    shell.CommandLineToArgvW.restype=ctypes.POINTER(ctypes.c_wchar_p)
    process.api.LocalFree.argtypes=[ctypes.c_void_p];process.api.LocalFree.restype=ctypes.c_void_p
    count=ctypes.c_int();argv=shell.CommandLineToArgvW(text,ctypes.byref(count))
    if not argv:raise ctypes.WinError(ctypes.get_last_error())
    try:
        if not 0<count.value<=1024:raise ValueError("Native argument count exceeds bound")
        result=[argv[i] for i in range(count.value)]
    finally:
        if process.api.LocalFree(ctypes.cast(argv,ctypes.c_void_p)):
            raise ctypes.WinError(ctypes.get_last_error())
    if process.exited():raise ProcessLookupError("Process exited during argument observation")
    return result
