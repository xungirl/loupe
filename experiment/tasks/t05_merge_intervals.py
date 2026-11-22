"""Task t05_merge_intervals (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement merge_intervals(intervals) taking a list of [start, end] pairs (any order) and returning a new list of merged, sorted, non-overlapping intervals. Touching intervals like [1,2] and [2,3] merge. Do not mutate the input.'

STUB = r'''
def merge_intervals(intervals):
    raise NotImplementedError
'''

REFERENCE = r'''
def merge_intervals(intervals):
    out = []
    for s, e in sorted(intervals):
        if out and s <= out[-1][1]:
            out[-1][1] = max(out[-1][1], e)
        else:
            out.append([s, e])
    return out
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(merge_intervals([[1, 3], [2, 6], [8, 10]]), [[1, 6], [8, 10]])


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(merge_intervals([]), [])
        self.assertEqual(merge_intervals([[1, 2], [2, 3]]), [[1, 3]])
        self.assertEqual(merge_intervals([[5, 6], [1, 2]]), [[1, 2], [5, 6]])
        self.assertEqual(merge_intervals([[1, 10], [2, 3], [4, 5]]), [[1, 10]])
        data = [[3, 4], [1, 2]]
        merge_intervals(data)
        self.assertEqual(data, [[3, 4], [1, 2]])


if __name__ == "__main__":
    unittest.main()
'''
