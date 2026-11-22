"""Task t20_lru_cache (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement class LRUCache(capacity) with get(key) -> value or -1 and put(key, value). When full, put evicts the least recently used key; both get and put count as a use.'

STUB = r'''
class LRUCache:
    def __init__(self, capacity):
        raise NotImplementedError

    def get(self, key):
        raise NotImplementedError

    def put(self, key, value):
        raise NotImplementedError
'''

REFERENCE = r'''
from collections import OrderedDict

class LRUCache:
    def __init__(self, capacity):
        self.capacity = capacity
        self.data = OrderedDict()

    def get(self, key):
        if key not in self.data:
            return -1
        self.data.move_to_end(key)
        return self.data[key]

    def put(self, key, value):
        self.data[key] = value
        self.data.move_to_end(key)
        if len(self.data) > self.capacity:
            self.data.popitem(last=False)
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        c = LRUCache(2)
        c.put(1, 1)
        self.assertEqual(c.get(1), 1)


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        c = LRUCache(2)
        c.put(1, 1)
        c.put(2, 2)
        self.assertEqual(c.get(1), 1)
        c.put(3, 3)
        self.assertEqual(c.get(2), -1)
        c.put(4, 4)
        self.assertEqual(c.get(1), -1)
        self.assertEqual(c.get(3), 3)
        self.assertEqual(c.get(4), 4)
        d = LRUCache(1)
        d.put('a', 1)
        d.put('a', 2)
        self.assertEqual(d.get('a'), 2)


if __name__ == "__main__":
    unittest.main()
'''
