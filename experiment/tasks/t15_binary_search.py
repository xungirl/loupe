"""Task t15_binary_search (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement binary_search(items, target) on a sorted list, returning the index of the FIRST occurrence of target or -1 if absent. Must be O(log n).'

STUB = r'''
def binary_search(items, target):
    raise NotImplementedError
'''

REFERENCE = r'''
def binary_search(items, target):
    lo, hi = 0, len(items)
    while lo < hi:
        mid = (lo + hi) // 2
        if items[mid] < target:
            lo = mid + 1
        else:
            hi = mid
    return lo if lo < len(items) and items[lo] == target else -1
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(binary_search([1, 3, 5, 7], 5), 2)


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(binary_search([], 1), -1)
        self.assertEqual(binary_search([1, 2, 2, 2, 3], 2), 1)
        self.assertEqual(binary_search([1, 3], 2), -1)
        self.assertEqual(binary_search([1, 3], 4), -1)
        self.assertEqual(binary_search(list(range(0, 1000, 2)), 998), 499)
        self.assertEqual(binary_search([5], 5), 0)


if __name__ == "__main__":
    unittest.main()
'''
