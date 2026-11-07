"""State polling must not block the writer's atomic replacement on Windows."""
import os
from pathlib import Path
import tempfile
import threading
import unittest

from fixtures.file_stamp import stamp


class FileStampTest(unittest.TestCase):
    # Repeated replacement overlaps continuous metadata observation; neither
    # path may fail and the observer must detect the final replacement.
    def test_should_allow_replacement_during_metadata_polling(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "state.json"
            target.write_text("old", encoding="utf-8")
            before = stamp(target)
            done, errors = threading.Event(), []

            def observe():
                try:
                    while not done.is_set():
                        stamp(target)
                except BaseException as error:
                    errors.append(error)

            observer = threading.Thread(target=observe)
            observer.start()
            try:
                for index in range(100):
                    replacement = Path(directory) / "next.json"
                    replacement.write_text(str(index), encoding="utf-8")
                    os.replace(replacement, target)
            finally:
                done.set()
                observer.join(timeout=5)
            self.assertFalse(observer.is_alive())
            self.assertEqual(errors, [])
            self.assertNotEqual(stamp(target), before)
            self.assertEqual(target.read_text(encoding="utf-8"), "99")


if __name__ == "__main__":
    unittest.main()
