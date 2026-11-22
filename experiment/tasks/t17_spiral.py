"""Task t17_spiral (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement spiral(matrix) returning the elements of a rectangular matrix in clockwise spiral order starting at the top-left.'

STUB = r'''
def spiral(matrix):
    raise NotImplementedError
'''

REFERENCE = r'''
def spiral(matrix):
    out = []
    m = [row[:] for row in matrix]
    while m:
        out.extend(m.pop(0))
        m = [list(r) for r in zip(*m)][::-1]
    return out
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(spiral([[1, 2, 3], [4, 5, 6], [7, 8, 9]]), [1, 2, 3, 6, 9, 8, 7, 4, 5])


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(spiral([]), [])
        self.assertEqual(spiral([[1]]), [1])
        self.assertEqual(spiral([[1, 2, 3, 4], [5, 6, 7, 8], [9, 10, 11, 12]]), [1, 2, 3, 4, 8, 12, 11, 10, 9, 5, 6, 7])
        self.assertEqual(spiral([[1], [2], [3]]), [1, 2, 3])


if __name__ == "__main__":
    unittest.main()
'''
