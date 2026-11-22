"""Task t13_caesar (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement caesar(text, shift) shifting ASCII letters by shift positions (wrapping within a-z / A-Z, preserving case); other characters are unchanged. shift may be negative or larger than 26.'

STUB = r'''
def caesar(text, shift):
    raise NotImplementedError
'''

REFERENCE = r'''
def caesar(text, shift):
    out = []
    for c in text:
        if 'a' <= c <= 'z':
            out.append(chr((ord(c) - 97 + shift) % 26 + 97))
        elif 'A' <= c <= 'Z':
            out.append(chr((ord(c) - 65 + shift) % 26 + 65))
        else:
            out.append(c)
    return ''.join(out)
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(caesar('abc', 1), 'bcd')


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(caesar('xyz', 3), 'abc')
        self.assertEqual(caesar('Hello, World!', 13), 'Uryyb, Jbeyq!')
        self.assertEqual(caesar('abc', -1), 'zab')
        self.assertEqual(caesar('abc', 27), 'bcd')
        self.assertEqual(caesar('', 5), '')
        self.assertEqual(caesar('é1', 4), 'é1')


if __name__ == "__main__":
    unittest.main()
'''
