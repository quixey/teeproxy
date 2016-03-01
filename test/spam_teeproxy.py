import requests
import argparse
import time


def main(args):
    for a in range(args.count):
        print "sending request {}".format(a)
        hack = time.time()
        requests.get("http://localhost:8888/health?id={}".format(a))
        print "Delay for request {}: {}".format(a, time.time()-hack)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="gor configurator")
    parser.add_argument("-c", "--count", type=int, help="number of queries to send", default=100)
    main(parser.parse_args())
