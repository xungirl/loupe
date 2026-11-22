"""Task t24_top_k (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement top_k_frequent(nums, k) returning the k most frequent numbers, most frequent first; ties broken by the smaller number first. If there are fewer than k distinct numbers return all of them.'

STUB = r'''
def top_k_frequent(nums, k):
    raise NotImplementedError
'''

REFERENCE = r'''
from collections import Counter

def top_k_frequent(nums, k):
    counts = Counter(nums)
    return sorted(counts, key=lambda x: (-counts[x], x))[:k]
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(top_k_frequent([1, 1, 1, 2, 2, 3], 2), [1, 2])


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(top_k_frequent([], 3), [])
        self.assertEqual(top_k_frequent([4, 4, 5, 5, 6], 2), [4, 5])
        self.assertEqual(top_k_frequent([1], 5), [1])
        self.assertEqual(top_k_frequent([3, 3, 2, 2, 1, 1, 1], 3), [1, 2, 3])
        self.assertEqual(top_k_frequent([-1, -1, 2], 1), [-1])


if __name__ == "__main__":
    unittest.main()
'''
