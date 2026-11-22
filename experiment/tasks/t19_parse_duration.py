"""Task t19_parse_duration (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = "Implement parse_duration(text) converting strings like '1h30m', '45s', '2h5s', '10m' to total seconds (int). Units h, m, s each appear at most once and in that order; whitespace is not allowed. Raise ValueError for empty or malformed input."

STUB = r'''
def parse_duration(text):
    raise NotImplementedError
'''

REFERENCE = r'''
import re

_PATTERN = re.compile(r'(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?')

def parse_duration(text):
    m = _PATTERN.fullmatch(text)
    if not text or not m:
        raise ValueError(text)
    h, mi, s = (int(g) if g else 0 for g in m.groups())
    return h * 3600 + mi * 60 + s
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(parse_duration('1h30m'), 5400)


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(parse_duration('45s'), 45)
        self.assertEqual(parse_duration('2h5s'), 7205)
        self.assertEqual(parse_duration('10m'), 600)
        for bad in ['', 'abc', '1x', '5m1h', '1h 2m', 'h']:
            with self.assertRaises(ValueError):
                parse_duration(bad)


if __name__ == "__main__":
    unittest.main()
'''
