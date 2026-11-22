"""Task t07_brackets (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement balanced(s) -> bool: True if every (, [ and { is closed by the matching bracket in the right order. Other characters are ignored.'

STUB = r'''
def balanced(s):
    raise NotImplementedError
'''

REFERENCE = r'''
def balanced(s):
    pairs = {')': '(', ']': '[', '}': '{'}
    stack = []
    for c in s:
        if c in '([{':
            stack.append(c)
        elif c in pairs:
            if not stack or stack.pop() != pairs[c]:
                return False
    return not stack
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertTrue(balanced('([]{})'))


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertFalse(balanced('(]'))
        self.assertFalse(balanced('(('))
        self.assertTrue(balanced(''))
        self.assertTrue(balanced('a(b[c]d)e'))
        self.assertFalse(balanced(')('))
        self.assertFalse(balanced('{[}]'))


if __name__ == "__main__":
    unittest.main()
'''
