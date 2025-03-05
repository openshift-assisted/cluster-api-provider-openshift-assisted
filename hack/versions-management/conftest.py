import sys
import os

# Add the current directory to the Python path
# This allows imports of modules from the hack directory during testing
# without requiring the package to be installed
sys.path.insert(0, os.path.abspath(os.path.dirname(__file__)))
