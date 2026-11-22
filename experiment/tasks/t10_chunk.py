"""Task t10_chunk (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement chunk(items, size) splitting a list into consecutive sublists of length size (the last may be shorter). Raise ValueError if size < 1.'

STUB = r'''
def chunk(items, size):
    raise NotImplementedError
'''

REFERENCE = r'''
def chunk(items, size):
    if size < 1:
        raise ValueError(size)
    return [items[i:i + size] for i in range(0, len(items), size)]
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(chunk([1, 2, 3, 4, 5], 2), [[1, 2], [3, 4], [5]])


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(chunk([], 3), [])
        self.assertEqual(chunk([1, 2, 3], 3), [[1, 2, 3]])
        self.assertEqual(chunk([1, 2, 3], 10), [[1, 2, 3]])
        with self.assertRaises(ValueError):
            chunk([1], 0)
        self.assertEqual(chunk(list(range(7)), 3), [[0, 1, 2], [3, 4, 5], [6]])


if __name__ == "__main__":
    unittest.main()
'''
