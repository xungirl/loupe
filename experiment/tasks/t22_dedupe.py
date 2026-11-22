"""Task t22_dedupe (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement dedupe(items) returning a new list with duplicates removed, keeping the first occurrence of each item and the original order. Items are hashable.'

STUB = r'''
def dedupe(items):
    raise NotImplementedError
'''

REFERENCE = r'''
def dedupe(items):
    seen = set()
    out = []
    for x in items:
        if x not in seen:
            seen.add(x)
            out.append(x)
    return out
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(dedupe([3, 1, 3, 2, 1]), [3, 1, 2])


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(dedupe([]), [])
        self.assertEqual(dedupe(['a', 'A', 'a']), ['a', 'A'])
        self.assertEqual(dedupe([1, 1, 1]), [1])
        self.assertEqual(dedupe([(1, 2), (1, 2), (2, 1)]), [(1, 2), (2, 1)])


if __name__ == "__main__":
    unittest.main()
'''
