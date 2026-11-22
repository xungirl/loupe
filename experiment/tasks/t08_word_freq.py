"""Task t08_word_freq (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement word_frequencies(text) returning a list of (word, count) tuples, words lower-cased and made of letters/apostrophes only (split on anything else), sorted by count descending then word ascending.'

STUB = r'''
def word_frequencies(text):
    raise NotImplementedError
'''

REFERENCE = r'''
import re
from collections import Counter

def word_frequencies(text):
    words = re.findall(r"[a-z']+", text.lower())
    return sorted(Counter(words).items(), key=lambda kv: (-kv[1], kv[0]))
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(word_frequencies('the cat the hat'), [('the', 2), ('cat', 1), ('hat', 1)])


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(word_frequencies(''), [])
        self.assertEqual(word_frequencies('Dog, dog! DOG?'), [('dog', 3)])
        self.assertEqual(word_frequencies("it's it's ok"), [("it's", 2), ('ok', 1)])
        self.assertEqual(word_frequencies('b a b a c'), [('a', 2), ('b', 2), ('c', 1)])
        self.assertEqual(word_frequencies('x1y'), [('x', 1), ('y', 1)])


if __name__ == "__main__":
    unittest.main()
'''
