"""Task t14_anagrams (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement group_anagrams(words) returning groups of words that are anagrams of each other. Each group is sorted alphabetically and the list of groups is sorted by its first word. Comparison is case-sensitive.'

STUB = r'''
def group_anagrams(words):
    raise NotImplementedError
'''

REFERENCE = r'''
def group_anagrams(words):
    groups = {}
    for w in words:
        groups.setdefault(''.join(sorted(w)), []).append(w)
    return sorted(sorted(g) for g in groups.values())
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(group_anagrams(['eat', 'tea', 'tan', 'ate', 'nat', 'bat']), [['ate', 'eat', 'tea'], ['bat'], ['nat', 'tan']])


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(group_anagrams([]), [])
        self.assertEqual(group_anagrams(['a']), [['a']])
        self.assertEqual(group_anagrams(['ab', 'ba', 'Ab']), [['Ab'], ['ab', 'ba']])
        self.assertEqual(group_anagrams(['', '']), [['', '']])


if __name__ == "__main__":
    unittest.main()
'''
