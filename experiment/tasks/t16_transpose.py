"""Task t16_transpose (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement transpose(matrix) for a rectangular list of lists, returning a new list of lists. transpose([]) == [].'

STUB = r'''
def transpose(matrix):
    raise NotImplementedError
'''

REFERENCE = r'''
def transpose(matrix):
    return [list(row) for row in zip(*matrix)]
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(transpose([[1, 2, 3], [4, 5, 6]]), [[1, 4], [2, 5], [3, 6]])


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(transpose([]), [])
        self.assertEqual(transpose([[1]]), [[1]])
        self.assertEqual(transpose([[1, 2]]), [[1], [2]])
        m = [[1, 2], [3, 4]]
        self.assertEqual(transpose(transpose(m)), m)


if __name__ == "__main__":
    unittest.main()
'''
