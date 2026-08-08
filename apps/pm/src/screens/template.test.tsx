import React from 'react';
import { Platform } from 'react-native';
import { render, fireEvent, screen, waitFor } from '@testing-library/react-native';
import TemplateScreen from '../../app/template/[id]';

// The instrument a manager tapped, previewed as a PDF and shared to be signed
// (#350). Before this the tap handed down a .docx nobody on a phone could open.

const mockPreview = jest.fn();
const mockShare = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: jest.fn(), push: jest.fn(), canGoBack: () => true }),
  useLocalSearchParams: () => ({ id: 't1' }),
}));

jest.mock('react-native-webview', () => {
  const { View } = jest.requireActual('react-native');
  return { WebView: (props: Record<string, unknown>) => <View testID="pdf" {...props} /> };
});

jest.mock('../data/preview', () => ({
  useTemplatePreview: () => mockPreview(),
}));

jest.mock('@dwellm8/mobile-shared', () => ({
  ...jest.requireActual('@dwellm8/mobile-shared'),
  apiFromEnv: () => null,
}));

const ready = {
  loading: false,
  linkMinutes: 10,
  fileUri: 'file:///cache/rent-agreement-preview.pdf',
  share: mockShare,
  reload: jest.fn(),
  preview: {
    kind: 'rent_agreement',
    name: 'Rent agreement, eleven months — India',
    filename: 'rent-agreement-preview.pdf',
    content_type: 'application/pdf',
    pdf_base64: 'JVBERi0xLjQK',
    download_url: 'https://signed.example/rent.pdf',
    expires_in_seconds: 600,
  },
};

beforeEach(() => {
  Platform.OS = 'ios';
  mockPreview.mockReset().mockReturnValue(ready);
  mockShare.mockReset();
});

// The page is the copy on this phone: a signed link expires in ten minutes,
// which is less time than it takes to read a lease deed.
it('shows the instrument as a PDF, not as a file to download', async () => {
  await render(<TemplateScreen />);
  await waitFor(() => expect(screen.getByTestId('pdf')).toBeTruthy());
  expect(screen.getByTestId('pdf').props.source.uri)
    .toBe('file:///cache/rent-agreement-preview.pdf');
});

it('falls back to the signed link when the file could not be written', async () => {
  mockPreview.mockReturnValue({ ...ready, fileUri: undefined });
  await render(<TemplateScreen />);
  expect(screen.getByTestId('pdf').props.source.uri).toBe('https://signed.example/rent.pdf');
});

// Saying a link expires when no link was shared is worse than saying nothing.
it('mentions the link only when there is one', async () => {
  mockPreview.mockReturnValue({ ...ready, preview: { ...ready.preview, download_url: undefined } });
  await render(<TemplateScreen />);
  expect(screen.queryByText(/10 minutes/)).toBeNull();
  expect(screen.getByText('Send it to be signed')).toBeTruthy();
});

it('shares the PDF with whoever has to sign it', async () => {
  await render(<TemplateScreen />);
  await fireEvent.press(screen.getByText('Send it to be signed'));
  expect(mockShare).toHaveBeenCalled();
});

// A manager pasting the link somewhere durable should know it dies in minutes.
it('says how long the link it shares stays alive', async () => {
  await render(<TemplateScreen />);
  expect(screen.getByText(/10 minutes/)).toBeTruthy();
});

// Android's system WebView has no PDF viewer, so a WebView there is a blank
// card. Hand the file to a real reader instead of pretending it rendered.
it('opens the PDF in a reader on Android rather than showing a blank card', async () => {
  Platform.OS = 'android';
  await render(<TemplateScreen />);

  expect(screen.queryByTestId('pdf')).toBeNull();
  await fireEvent.press(screen.getByText('Open the preview'));
  expect(mockShare).toHaveBeenCalled();
});

it('offers a retry when the preview will not print', async () => {
  const reload = jest.fn();
  mockPreview.mockReturnValue({
    loading: false, linkMinutes: 0, share: mockShare, reload,
    error: 'could not print the preview',
  });
  await render(<TemplateScreen />);
  await fireEvent.press(screen.getByText('Try again'));
  expect(reload).toHaveBeenCalled();
});
