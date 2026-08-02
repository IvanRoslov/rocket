# Expo HAS CHANGED

Read the exact versioned docs at https://docs.expo.dev/versions/v57.0.0/ before writing any code.

# Никаких тестов внутри `app/`

`app/` — это дерево маршрутов expo-router, и его `require.context` втягивает в
бандл каждый `.tsx` оттуда, включая `*.test.tsx`. Тест тянет за собой
`@testing-library/react-native`, тот — node-модуль `console`, которого в
React Native нет, и `make apk` падает на `createBundleReleaseJsAndAssets`.
Тесты экранов живут в `__tests__/` и импортируют экраны как `../../app/...`.
