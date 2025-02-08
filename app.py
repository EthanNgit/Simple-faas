from flask import Flask, jsonify
import os

app = Flask(__name__)
fun_code = os.environ.get('FUN_CODE', '')

# define the custom function
try:
    exec(fun_code)
except:
    print(f"Could not load custom function {e}")

@app.route('/invoke', methods=['POST'])
def invoke():
    data = request.get_json()
    params = data.get('params', {})
    try:
        result = user_function(**params)
        return jsonify({'result': result, 'error': None})
    except Exception as e:
        return jsonify({'result': None, 'error': str(e)}), 500

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000)