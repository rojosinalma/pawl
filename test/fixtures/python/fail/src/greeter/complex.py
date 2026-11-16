"""High cyclomatic complexity."""


def classify(a: int, b: int, c: int) -> str:
    out = ""
    for i in range(a):
        if i % 2 == 0:
            if b > 0:
                for j in range(b):
                    if j % 3 == 0:
                        if c > 2:
                            out += "x"
                        elif c > 1:
                            out += "y"
                        elif c > 0:
                            out += "z"
                        elif c < -1:
                            out += "q"
                        elif c < -2:
                            out += "r"
                        elif c < -3:
                            out += "s"
                        else:
                            while len(out) < 10:
                                out += "w"
            elif b < -1:
                out += "v"
            elif b < -2:
                out += "u"
            elif b < -3:
                out += "t"
    return out
