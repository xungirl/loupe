"""Task t11_rle_encode (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = "Implement rle_encode(s) run-length encoding a string: each run becomes the character followed by the run length, e.g. 'aaabcc' -> 'a3b1c2'. Empty string -> ''."

STUB = r'''
def rle_encode(s):
    raise NotImplementedError
'''

REFERENCE = r'''
def rle_encode(s):
    out = []
    i = 0
    while i < len(s):
        j = i
        while j < len(s) and s[j] == s[i]:
            j += 1
        out.append(f'{s[i]}{j - i}')
        i = j
    return ''.join(out)
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(rle_encode('aaabcc'), 'a3b1c2')


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(rle_encode(''), '')
        self.assertEqual(rle_encode('a'), 'a1')
        self.assertEqual(rle_encode('aaaaaaaaaaaa'), 'a12')
        self.assertEqual(rle_encode('abab'), 'a1b1a1b1')
        self.assertEqual(rle_encode('  x'), ' 2x1')


if __name__ == "__main__":
    unittest.main()
'''
