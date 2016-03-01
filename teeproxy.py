from flask import Flask
import flask
import argparse
import requests
import ujson
import logging
import random

app = Flask(__name__)


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

    print "Request seen: {} with body {}".format(flask.request.full_path, ujson.dumps(flask.request.json, indent=2))
    return flask.Response(status=200, response=flask.request.full_path + '\n' + ujson.dumps(flask.request.json, indent=2)+ '\n')


def main():
    parser = argparse.ArgumentParser(description="gor configurator")
    parser.add_argument("-i", "--listen_port", action="store", help="Select Input Port", default=':8880')
    parser.add_argument("-a", "--target", action="store", help="Proxy traffic to selected host[:port]",
                        default='localhost:80')
    parser.add_argument("-b", "--alt_target", action="store", help="Forward a copy of all traffic to selected host[:port]",
                        required=False)
    args = parser.parse_args()
    if ":" in args.listen_port:
        args.listen_port = args.listen_port.split(":")[1]
    logging.info("target: {}".format(args.target))
    if "http" not in args.target:
        args.target = "https://" + args.target

    app.args = args

    app.run(port=args.listen_port)


if __name__ == '__main__':
    main()

