import React from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
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
  const { loading, error, preview, linkMinutes, share, reload } = useTemplatePreview(id ?? '');

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

        {preview?.download_url ? (
          <Card padded={false} style={s.paper}>
            <WebView
              source={{ uri: preview.download_url }}
              style={s.pdf}
              startInLoadingState
              renderLoading={() => <View style={s.waiting}><ActivityIndicator /></View>}
            />
          </Card>
        ) : null}

        {preview ? (
          <>
            <Button label="Send it to be signed" onPress={share} />
            <Card>
              <Text style={s.note}>
                The link this shares is signed for whoever opens it and stops working after{' '}
                {linkMinutes} minutes, so send the file itself to anyone who needs to keep it.
              </Text>
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
