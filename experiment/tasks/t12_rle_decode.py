"""Task t12_rle_decode (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = "Implement rle_decode(s) inverting run-length encoding where each character is followed by a positive decimal count (possibly several digits), e.g. 'a3b1c12' -> 'aaab' + 'c'*12. Characters are never digits."

STUB = r'''
def rle_decode(s):
    raise NotImplementedError
'''

REFERENCE = r'''
def rle_decode(s):
    out = []
    i = 0
    while i < len(s):
        ch = s[i]
        j = i + 1
        while j < len(s) and s[j].isdigit():
            j += 1
        out.append(ch * int(s[i + 1:j]))
        i = j
    return ''.join(out)
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(rle_decode('a3b1'), 'aaab')


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(rle_decode(''), '')
        self.assertEqual(rle_decode('c12'), 'c' * 12)
        self.assertEqual(rle_decode('a1b1a1'), 'aba')
        self.assertEqual(rle_decode(' 2x1'), '  x')
        self.assertEqual(rle_decode('z100'), 'z' * 100)


if __name__ == "__main__":
    unittest.main()
'''
