"""Observe replacements using directory entries, without opening target files."""
import os


def stamp(path):
    # On Windows DirEntry.stat uses metadata supplied by FindFirstFile/FindNextFile
    # for regular files. Querying the file itself can still collide with rename.
    with os.scandir(path.parent) as entries:
        for entry in entries:
            if entry.name == path.name:
                return entry.stat(follow_symlinks=False).st_mtime_ns
    raise FileNotFoundError(path)
