import { fireEvent, render, screen } from '@testing-library/react-native'
import { router } from 'expo-router'
import { AuthLostBanner } from './AuthLostBanner'

jest.mock('expo-router', () => ({ router: { push: jest.fn() } }))

it('shows the re-pair message and navigates to the pair screen', async () => {
  await render(<AuthLostBanner />)
  expect(screen.getByText(/Доступ к серверу отозван или не настроен/)).toBeTruthy()
  await fireEvent.press(screen.getByText('Подключить'))
  expect(router.push).toHaveBeenCalledWith('/servers?pair=1')
})
