from flask import Flask
import flask
import argparse
import requests
import ujson
import logging
import random
import time
import math
app = Flask(__name__)

PRECISION = 1000.0


def proxy_request(request, host, request_id):
    url = host + request.full_path
    resp = requests.request(
        method=request.method,
        url=url,
        headers=request.headers,
        params=request.args,
        data=request.data,
        allow_redirects=True
    )
    logging.info("Sent request id {} to : {}".format(request_id, flask.current_app.args.alt_target))
    return resp


def generate_random_string(length=32, include_numbers=True, include_letters=True):
    """Returns a nice-looking GUID for partner_secrets (recovered from Legacy Quixey Partner Portal Code).
    :param length: (int) length of random string.
    :param include_numbers: (boolean) Allows [1..0] to be included in the string.
    :param include_letters: (boolean) Allows any of 'abcdefghkmnpqrstuvwxyz' to be included in the string.
    :return: GUID (string)
    """
    charset = ''
    if include_numbers:
        # Exclude 0
        charset += '123456789'
    if include_letters:
        # Exclude i, j, l, o
        charset += 'abcdefghkmnpqrstuvwxyz'

    assert len(charset) > 1, 'Bad charset for guid'

    return ''.join(
        random.choice(charset) for i in range(length)
    )


@app.route('/', defaults={'path': ''}, methods=["GET", "POST"])
@app.route('/<path:path>', methods=["GET", "POST"])
def catch_all(path):

    request_id = generate_random_string()
    logging.info("request id {} received: {}".format(request_id, path))

    '''
    gevent.spawn(
        proxy_request(flask.request, flask.current_app.args.alt_target, request_id)
    )

    reply = proxy_request(flask.request, flask.current_app.args.target, request_id)
    '''
    _min = abs(flask.current_app.args.min_latency)
    _max = abs(flask.current_app.args.max_latency)
    if flask.current_app.args.gausian == 'u':
        print 'uniform distribution'
        delay = random.uniform(
            int(_min * PRECISION),
            int(_max * PRECISION)
        ) / PRECISION + _min

    else:
        g = flask.current_app.args.gausian
        print 'gausian normal distribution: {}'.format(g)

        if g == 'm':
            mu = (_max - _min)/2.0
            sigma = mu - flask.current_app.args.min_latency
            delay = random.gauss(mu, sigma)
        else:
            mu = 0
            sigma = 1
            delay = random.lognormvariate(mu, sigma)
            while delay > 10:
                delay /= 10
            span = _max - _min
            if span > 0:
                scale = span / 10
            else:
                scale = 1

            delay = scale * delay + _min

            if g == 'x':
                delay *= -1
                delay += (_min + _max)

    delay = max(delay, 0)




    print "Request seen: {} with body {}\ndelay: {}".format(flask.request.full_path, ujson.dumps(flask.request.json, indent=2), delay)
    time.sleep(delay)
    return flask.Response(status=200, response=flask.request.full_path + '\n' + ujson.dumps(flask.request.json, indent=2)+ '\n')


def main():
    parser = argparse.ArgumentParser(description="variable reflector  configurator")
    parser.add_argument("-i", "--listen_port",
                        action="store",
                        help="Select Input Port",
                        default=':8880')
    parser.add_argument("-l", "--min_latency",
                        action="store",
                        help="Minimum Local Reflection Latency",
                        type=float,
                        default=0.0)
    parser.add_argument("-x", "--max_latency",
                        action="store",
                        help="Minimum Local Reflection Latency",
                        type=float,
                        default=0.0)
    parser.add_argument("-g", "--gausian",
                        action="store",
                        help="[h,m,l,o] Favour High/Low/Medium-latency queries, or Uniform (linear) ['u'] Distribution",
                        choices=["x", "m", "l", "u"],
                        default="u")
    args = parser.parse_args()
    if ":" in args.listen_port:
        args.listen_port = args.listen_port.split(":")[1]

    app.args = args

    app.run(port=args.listen_port)


if __name__ == '__main__':
    main()

