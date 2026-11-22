"""Task t04_int_to_roman (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement int_to_roman(n) for 1 <= n <= 3999 using standard subtractive notation (4 = IV, 9 = IX, 40 = XL, ...). Raise ValueError outside that range.'

STUB = r'''
def int_to_roman(n):
    raise NotImplementedError
'''

REFERENCE = r'''
def int_to_roman(n):
    if not 1 <= n <= 3999:
        raise ValueError(n)
    table = [(1000, 'M'), (900, 'CM'), (500, 'D'), (400, 'CD'), (100, 'C'), (90, 'XC'), (50, 'L'), (40, 'XL'), (10, 'X'), (9, 'IX'), (5, 'V'), (4, 'IV'), (1, 'I')]
    out = ''
    for value, sym in table:
        while n >= value:
            out += sym
            n -= value
    return out
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(int_to_roman(3), 'III')


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(int_to_roman(4), 'IV')
        self.assertEqual(int_to_roman(1994), 'MCMXCIV')
        self.assertEqual(int_to_roman(3999), 'MMMCMXCIX')
        self.assertEqual(int_to_roman(40), 'XL')
        with self.assertRaises(ValueError):
            int_to_roman(0)
        with self.assertRaises(ValueError):
            int_to_roman(4000)


if __name__ == "__main__":
    unittest.main()
'''
