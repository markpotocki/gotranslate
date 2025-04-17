import requests
import os
import pytest

@pytest.fixture
def api_key():
    return os.getenv('API_KEY', '')


@pytest.fixture
def base_url():
    return os.getenv('BASE_URL', 'http://127.0.0.1:3000')


@pytest.fixture
def headers(api_key):
    return {
        'X-Api-Key': api_key
    }


@pytest.fixture
def translate_url(base_url):
    return f'{base_url}/translate'


def test_translate_content(translate_url, headers):
    source_language = 'en'
    target_language = 'es'
    text = 'Hello, world!'

    response = requests.post(
        translate_url,
        json={
            'source_language': source_language,
            'target_language': target_language,
            'text': text
        },
        headers=headers
    )

    assert response.status_code == 200, f"Expected status code 200, got {response.status_code}"
    response_data = response.json()
    assert 'translated_text' in response_data, "Response does not contain 'translated_text'"
    assert response_data['translated_text'] == '¡Hola, mundo!', "Translation did not match expected output"


def test_translate_content_with_invalid_language(translate_url, headers):
    source_language = 'xx'
    target_language = 'es'
    text = 'Hello, world!'

    response = requests.post(
        translate_url,
        json={
            'source_language': source_language,
            'target_language': target_language,
            'text': text
        },
        headers=headers
    )

    assert response.status_code == 400, f"Expected status code 400, got {response.status_code}"
    response_data = response.json()
    assert 'error' in response_data, "Response does not contain 'error'"
    assert response_data['error'] == 'Invalid source language', "Error message did not match expected output"


def test_translate_content_with_empty_text(translate_url, headers):
    source_language = 'en'
    target_language = 'es'
    text = ''

    response = requests.post(
        translate_url,
        json={
            'source_language': source_language,
            'target_language': target_language,
            'text': text
        },
        headers=headers
    )

    assert response.status_code == 400, f"Expected status code 400, got {response.status_code}"
    response_data = response.json()
    assert 'error' in response_data, "Response does not contain 'error'"
    assert response_data['error'] == 'Text cannot be empty', "Error message did not match expected output"


def test_translate_content_with_html(translate_url, headers):
    source_language = 'en'
    target_language = 'es'
    text = '<p>Hello, <strong>world</strong>!</p>'

    response = requests.post(
        translate_url,
        json={
            'source_language': source_language,
            'target_language': target_language,
            'text': text
        },
        headers=headers
    )

    assert response.status_code == 200, f"Expected status code 200, got {response.status_code}"
    response_data = response.json()
    assert 'translated_text' in response_data, "Response does not contain 'translated_text'"
    assert response_data['translated_text'] == '<p>¡Hola, <strong>mundo</strong>!</p>', "Translation did not match expected output"
