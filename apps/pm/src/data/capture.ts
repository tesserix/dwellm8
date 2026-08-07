import * as ImagePicker from 'expo-image-picker';

/**
 * Photograph an identity document (#318). The bytes go straight to the scan
 * endpoint and are kept nowhere — not on the device, not in the library.
 *
 * Permission is asked at the moment of use with the reason on screen behind
 * it, and a refusal is an ordinary answer: the fields are typed by hand.
 */
export class CaptureRefused extends Error {}

const options: ImagePicker.ImagePickerOptions = {
  mediaTypes: ['images'],
  base64: true,
  quality: 0.8,
  // A document is read, not cropped: the whole card has to stay in frame.
  allowsEditing: false,
  exif: false,
};

/** The photograph itself, and the file it was written to. */
export type Photograph = { base64: string; uri: string; contentType: string };

export async function photographDocument(): Promise<Photograph> {
  const permission = await ImagePicker.requestCameraPermissionsAsync();
  if (!permission.granted) {
    throw new CaptureRefused('Dwellm8 cannot open the camera — type the details in instead');
  }
  return read(await ImagePicker.launchCameraAsync(options));
}

export async function pickDocument(): Promise<Photograph> {
  const permission = await ImagePicker.requestMediaLibraryPermissionsAsync();
  if (!permission.granted) {
    throw new CaptureRefused('Dwellm8 cannot open your photos — type the details in instead');
  }
  return read(await ImagePicker.launchImageLibraryAsync(options));
}

function read(result: ImagePicker.ImagePickerResult): Photograph {
  if (result.canceled) throw new CaptureRefused('No photograph taken');
  const shot = result.assets[0];
  if (!shot?.base64) throw new Error('that photograph could not be read');
  return { base64: shot.base64, uri: shot.uri, contentType: shot.mimeType ?? 'image/jpeg' };
}
