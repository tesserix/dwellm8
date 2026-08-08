import React from 'react';
import { View, Text, StyleSheet, ActivityIndicator, Platform } from 'react-native';
import { useLocalSearchParams } from 'expo-router';
import { WebView } from 'react-native-webview';
import {
  BackHeader, Button, Card, ErrorState, Screen,
  color, font, radius, space, useBack,
} from '@dwellm8/mobile-shared';
import { useTemplatePreview } from '../../src/data/preview';

/**
 * One instrument, previewed and sent to be signed (#350).
 *
 * Signing is still on paper: what the share sheet hands over is the blank
 * form, and the executed copy comes back as an upload.
 */
export default function TemplateScreen() {
  const goBack = useBack('/agreements');
  const { id } = useLocalSearchParams<{ id?: string }>();
  const { loading, error, preview, fileUri, linkMinutes, share, reload } = useTemplatePreview(id ?? '');
  // Android's system WebView cannot render a PDF, so there it opens in a reader.
  const page = Platform.OS === 'android' ? undefined : fileUri ?? preview?.download_url;

  return (
    <>
      <BackHeader
        title={preview?.name ?? 'Instrument'}
        subtitle="Read it, send it to be signed, file the signed copy"
        onBack={goBack}
      />
      <Screen>
        {loading ? <View style={s.waiting}><ActivityIndicator /></View> : null}
        {error ? <ErrorState error={error} onRetry={reload} /> : null}

        {page ? (
          <Card padded={false} style={s.paper}>
            <WebView
              source={{ uri: page }}
              allowFileAccess
              allowFileAccessFromFileURLs
              style={s.pdf}
              startInLoadingState
              renderLoading={() => <View style={s.waiting}><ActivityIndicator /></View>}
            />
          </Card>
        ) : null}

        {preview ? (
          <>
            {page ? null : <Button label="Open the preview" tone="secondary" onPress={share} />}
            <Button label="Send it to be signed" onPress={share} />
            <Card>
              <Text style={s.note}>
                This sends the PDF itself, so whoever has to sign it keeps their own copy.
              </Text>
              {preview.download_url ? (
                <Text style={s.note}>
                  The link behind it is signed for whoever opens it and stops working after{' '}
                  {linkMinutes} minutes.
                </Text>
              ) : null}
              <Text style={s.note}>
                Both parties sign the printed copy. Upload the signed scan against the property so
                it sits beside the deed.
              </Text>
            </Card>
          </>
        ) : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  waiting: { paddingVertical: space(8), alignItems: 'center' },
  paper: { height: 520, overflow: 'hidden', borderRadius: radius.md },
  pdf: { flex: 1, backgroundColor: color.card },
  note: { ...font.small, color: color.inkSoft, lineHeight: 19, marginTop: space(2) },
});
