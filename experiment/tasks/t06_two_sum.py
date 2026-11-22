"""Task t06_two_sum (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement two_sum(nums, target) returning a tuple (i, j) with i < j such that nums[i] + nums[j] == target. If several pairs exist return the one with the smallest j, then smallest i. Return None if there is no pair.'

STUB = r'''
def two_sum(nums, target):
    raise NotImplementedError
'''

REFERENCE = r'''
def two_sum(nums, target):
    first = {}
    for j, x in enumerate(nums):
        if target - x in first:
            return (first[target - x], j)
        first.setdefault(x, j)
    return None
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(two_sum([2, 7, 11, 15], 9), (0, 1))


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(two_sum([3, 2, 4], 6), (1, 2))
        self.assertEqual(two_sum([3, 3], 6), (0, 1))
        self.assertIsNone(two_sum([1, 2], 7))
        self.assertIsNone(two_sum([], 0))
        self.assertEqual(two_sum([1, 5, 1, 5], 6), (0, 1))
        self.assertEqual(two_sum([-1, -2, -3], -5), (1, 2))


if __name__ == "__main__":
    unittest.main()
'''
