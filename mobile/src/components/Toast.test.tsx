import { fireEvent, render, screen } from '@testing-library/react-native'
import { Pressable, Text } from 'react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { ToastProvider, useToast } from './Toast'

function Trigger() {
  const toast = useToast()
  return (
    <Pressable onPress={() => toast.show('boom happened')}>
      <Text>trigger</Text>
    </Pressable>
  )
}

describe('Toast', () => {
  it('shows the message after show()', async () => {
    await render(
      <SafeAreaProvider initialMetrics={{ frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 47, left: 0, right: 0, bottom: 34 } }}>
        <ToastProvider>
          <Trigger />
        </ToastProvider>
      </SafeAreaProvider>,
    )
    expect(screen.queryByText('boom happened')).toBeNull()
    await fireEvent.press(screen.getByText('trigger'))
    expect(screen.getByText('boom happened')).toBeTruthy()
  })
})
