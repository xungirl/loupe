"""Task t23_camel_to_snake (self-written for Loupe, MIT licensed with the repo)."""

PROMPT = "Implement camel_to_snake(name) converting CamelCase or camelCase identifiers to snake_case. Runs of capitals are treated as one word: 'HTTPServerError' -> 'http_server_error', 'getHTTP' -> 'get_http'. Digits stay attached to the preceding word: 'version2Beta' -> 'version2_beta'."

STUB = r'''
def camel_to_snake(name):
    raise NotImplementedError
'''

REFERENCE = r'''
import re

def camel_to_snake(name):
    s = re.sub(r'([A-Z]+)([A-Z][a-z])', r'\1_\2', name)
    s = re.sub(r'([a-z0-9])([A-Z])', r'\1_\2', s)
    return s.lower()
'''

VISIBLE_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(camel_to_snake('helloWorld'), 'hello_world')


if __name__ == "__main__":
    unittest.main()
'''

HIDDEN_TESTS = r'''
import unittest

from solution import *  # noqa: F401,F403


class Tests(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(camel_to_snake('HTTPServerError'), 'http_server_error')
        self.assertEqual(camel_to_snake('getHTTP'), 'get_http')
        self.assertEqual(camel_to_snake('version2Beta'), 'version2_beta')
        self.assertEqual(camel_to_snake('Simple'), 'simple')
        self.assertEqual(camel_to_snake('already_snake'), 'already_snake')
        self.assertEqual(camel_to_snake('XMLHttpRequest'), 'xml_http_request')


if __name__ == "__main__":
    unittest.main()
'''
