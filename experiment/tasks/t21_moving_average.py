"""Task t21_moving_average (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement moving_average(nums, k) returning the list of averages (floats) of each window of k consecutive numbers. Return [] if k > len(nums); raise ValueError if k < 1. Must be O(n).'

STUB = r'''
def moving_average(nums, k):
    raise NotImplementedError
'''

REFERENCE = r'''
def moving_average(nums, k):
    if k < 1:
        raise ValueError(k)
    if k > len(nums):
        return []
    s = sum(nums[:k])
    out = [s / k]
    for i in range(k, len(nums)):
        s += nums[i] - nums[i - k]
        out.append(s / k)
    return out
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(moving_average([1, 2, 3, 4], 2), [1.5, 2.5, 3.5])


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(moving_average([1, 2], 3), [])
        self.assertEqual(moving_average([5], 1), [5.0])
        with self.assertRaises(ValueError):
            moving_average([1], 0)
        self.assertEqual(moving_average([0, 0, 3, 3], 3), [1.0, 2.0])
        self.assertEqual(len(moving_average(list(range(10000)), 100)), 9901)


if __name__ == "__main__":
    unittest.main()
'''
