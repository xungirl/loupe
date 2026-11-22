"""Task t18_common_prefix (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = "Implement common_prefix(words) returning the longest common prefix of a list of strings ('' for an empty list)."

STUB = r'''
def common_prefix(words):
    raise NotImplementedError
'''

REFERENCE = r'''
def common_prefix(words):
    if not words:
        return ''
    lo, hi = min(words), max(words)
    i = 0
    while i < len(lo) and lo[i] == hi[i]:
        i += 1
    return lo[:i]
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(common_prefix(['flower', 'flow', 'flight']), 'fl')


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(common_prefix([]), '')
        self.assertEqual(common_prefix(['dog', 'racecar']), '')
        self.assertEqual(common_prefix(['same', 'same']), 'same')
        self.assertEqual(common_prefix(['', 'a']), '')
        self.assertEqual(common_prefix(['interview', 'internet', 'interval']), 'inter')


if __name__ == "__main__":
    unittest.main()
'''
