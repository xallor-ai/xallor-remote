import koffi from "koffi";

// kernel32 is a Windows KnownDLL. Use Node's own libuv descriptor conversion,
// not a separately loaded CRT's descriptor table.
export function openWindowsImage(binary: string): number {
  const kernel = koffi.load("kernel32.dll");
  const create = kernel.func("intptr_t __stdcall CreateFileW(str16 name, uint32_t access, uint32_t sharing, void *security, uint32_t disposition, uint32_t flags, intptr_t template)");
  const close = kernel.func("int __stdcall CloseHandle(intptr_t handle)");
  const info = kernel.func("int __stdcall GetFileInformationByHandleEx(intptr_t handle, int kind, void *buffer, uint32_t size)");
  const uv = koffi.load(process.execPath);
  const adopt = uv.func("int uv_open_osfhandle(intptr_t handle)");
  const handle = create(binary, 0x80000000, 1, null, 3, 0x00200000, 0);
  if (handle === -1 || handle === -1n) throw new Error("Cannot lock bootstrap image against writes and replacement");
  try {
    const attributes = Buffer.alloc(8);
    if (!info(handle, 9, attributes, attributes.length) || (attributes.readUInt32LE() & (0x400 | 0x10))) {
      throw new Error("Bootstrap image cannot be a directory or reparse point");
    }
    const fd: number = adopt(handle);
    if (fd < 0) throw new Error("Cannot inherit protected bootstrap image");
    return fd; // fs.closeSync now owns the handle; child stdio[3] inherits it.
  } catch (error) { close(handle); throw error; }
}
