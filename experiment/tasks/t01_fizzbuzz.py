"""Task t01_fizzbuzz (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = "Implement fizzbuzz(n) returning a list of strings for 1..n: 'Fizz' for multiples of 3, 'Buzz' for multiples of 5, 'FizzBuzz' for both, otherwise the number as a string. n <= 0 returns []."

STUB = r'''
def fizzbuzz(n):
    raise NotImplementedError
'''

REFERENCE = r'''
def fizzbuzz(n):
    out = []
    for i in range(1, n + 1):
        s = ('Fizz' if i % 3 == 0 else '') + ('Buzz' if i % 5 == 0 else '')
        out.append(s or str(i))
    return out
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(fizzbuzz(5), ['1', '2', 'Fizz', '4', 'Buzz'])


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(fizzbuzz(15)[-1], 'FizzBuzz')
        self.assertEqual(fizzbuzz(0), [])
        self.assertEqual(fizzbuzz(-3), [])
        self.assertEqual(len(fizzbuzz(100)), 100)
        self.assertEqual(fizzbuzz(30)[29], 'FizzBuzz')
        self.assertEqual(fizzbuzz(10)[8], 'Fizz')


if __name__ == "__main__":
    unittest.main()
'''
