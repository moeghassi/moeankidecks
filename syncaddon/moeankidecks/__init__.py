from .version import __version__

try:
    import aqt  # noqa: F401
except ModuleNotFoundError:
    # Allow the pure synchronization core to be tested outside Anki.
    pass
else:
    from .runtime import register

    register()
