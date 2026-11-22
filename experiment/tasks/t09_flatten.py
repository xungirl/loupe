"""Task t09_flatten (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement flatten(items) that flattens arbitrarily nested lists and tuples into a single flat list, preserving order. Strings are atoms, not sequences.'

STUB = r'''
def flatten(items):
    raise NotImplementedError
'''

REFERENCE = r'''
def flatten(items):
    out = []
    for x in items:
        if isinstance(x, (list, tuple)):
            out.extend(flatten(x))
        else:
            out.append(x)
    return out
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(flatten([1, [2, [3, 4]], 5]), [1, 2, 3, 4, 5])


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(flatten([]), [])
        self.assertEqual(flatten([[[]]]), [])
        self.assertEqual(flatten(['ab', ['cd']]), ['ab', 'cd'])
        self.assertEqual(flatten([(1, 2), [3, (4,)]]), [1, 2, 3, 4])
        self.assertEqual(flatten([[[[[7]]]]]), [7])


if __name__ == "__main__":
    unittest.main()
'''
