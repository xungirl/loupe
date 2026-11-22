"""Task t03_roman_to_int (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = 'Implement roman_to_int(s) converting a valid Roman numeral (I, V, X, L, C, D, M, with subtractive forms like IV, IX, XL, XC, CD, CM) in the range 1..3999 to an int.'

STUB = r'''
def roman_to_int(s):
    raise NotImplementedError
'''

REFERENCE = r'''
def roman_to_int(s):
    v = {'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100, 'D': 500, 'M': 1000}
    total = 0
    for i, c in enumerate(s):
        if i + 1 < len(s) and v[c] < v[s[i + 1]]:
            total -= v[c]
        else:
            total += v[c]
    return total
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(roman_to_int('III'), 3)


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(roman_to_int('IV'), 4)
        self.assertEqual(roman_to_int('IX'), 9)
        self.assertEqual(roman_to_int('LVIII'), 58)
        self.assertEqual(roman_to_int('MCMXCIV'), 1994)
        self.assertEqual(roman_to_int('MMMCMXCIX'), 3999)
        self.assertEqual(roman_to_int('XLII'), 42)


if __name__ == "__main__":
    unittest.main()
'''
