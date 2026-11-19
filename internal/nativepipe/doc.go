// Package nativepipe provides nonblocking native reads of owned pipe handles.
// Prepare configures a descriptor once. ReadAvailable returns (0, nil) while no
// bytes are available, and io.EOF when all writers have closed. Callers retain
// the file, own cancellation/bounds, and close it after their final native read.
// Capture File.Fd only once before Prepare: subsequent Fd calls can restore
// blocking mode on Unix. This package neither transfers nor closes ownership.
package nativepipe
