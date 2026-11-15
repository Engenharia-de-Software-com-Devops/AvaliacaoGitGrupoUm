import tempfile
import unittest
from pathlib import Path

from line_coverage import measure


class LineCoverageTest(unittest.TestCase):
    def test_multiline_blocks_and_uncovered_lines(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "fixture.go"
            source.write_text("func sample() {\n    value := 1\n\n    value++ // comment\n    return\n}\n")
            profile = f"mode: set\n{source}:1.15,4.12 2 1\n{source}:5.5,5.11 1 0\n"
            result = measure(profile, [str(source)])
            self.assertEqual(result[str(source)], {2: True, 4: True, 5: False})

    def test_executed_block_on_same_line_marks_line_covered(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "fixture.go"
            source.write_text("first(); second()\n")
            profile = f"mode: set\n{source}:1.1,1.8 1 0\n{source}:1.10,1.18 1 1\n"
            self.assertEqual(measure(profile, [str(source)])[str(source)], {1: True})


if __name__ == "__main__":
    unittest.main()
