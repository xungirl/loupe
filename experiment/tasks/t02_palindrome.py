"""Task t02_palindrome (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement is_palindrome(s) -> bool. Ignore case and every character that is not a letter or digit. The empty string is a palindrome.'

STUB = r'''
def is_palindrome(s):
    raise NotImplementedError
'''

REFERENCE = r'''
def is_palindrome(s):
    t = [c.lower() for c in s if c.isalnum()]
    return t == t[::-1]
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertTrue(is_palindrome('A man, a plan, a canal: Panama'))


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertFalse(is_palindrome('race a car'))
        self.assertTrue(is_palindrome(''))
        self.assertTrue(is_palindrome('  !! '))
        self.assertTrue(is_palindrome('No lemon, no melon'))
        self.assertFalse(is_palindrome('ab'))
        self.assertTrue(is_palindrome('12321'))
        self.assertFalse(is_palindrome('0P'))


if __name__ == "__main__":
    unittest.main()
'''
