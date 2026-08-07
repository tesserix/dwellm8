import React, { useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator, Alert } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, ListRow, SectionTitle, StatusPill,
  apiFromEnv, color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import { useOwnershipEvidence } from '../src/data/ownership';
import { deedKinds, fileDeed } from '../src/data/deeds';
import { photographDocument, pickDocument, CaptureRefused } from '../src/data/capture';

/**
 * What proves the owner may let this property (#339): the deed, or the power
 * of attorney under which somebody else acts for the owner. The rest is worth
 * holding and blocks nothing.
 */
export default function DeedsScreen() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { id } = useLocalSearchParams<{ id?: string }>();
  const { loading, error, documents, proven, held, advisory } = useOwnershipEvidence(id);
  const [filing, setFiling] = useState('');

  function add(kind: string, label: string) {
    if (!id) return;
    Alert.alert(label, 'Photograph the document, or choose a scan already on this phone.', [
      { text: 'Photograph', onPress: () => run(kind, label, photographDocument) },
      { text: 'Choose a scan', onPress: () => run(kind, label, pickDocument) },
      { text: 'Cancel', style: 'cancel' },
    ]);
  }

  async function run(kind: string, label: string, take: () => Promise<{ uri: string; contentType: string }>) {
    const api = apiFromEnv();
    if (!api || !id) return;
    setFiling(kind);
    try {
      const shot = await take();
      await fileDeed(api, id, { kind, uri: shot.uri, contentType: shot.contentType });
      // The write answers the question the screen is asking, so it re-reads.
      router.replace(`/deeds?id=${id}`);
    } catch (err) {
      if (!(err instanceof CaptureRefused)) Alert.alert(label, (err as Error).message);
    } finally {
      setFiling('');
    }
  }

  return (
    <>
      <BackHeader
        title="Ownership"
        subtitle="What proves this property may be let"
        onBack={goBack}
      />
      <Screen>
        {loading ? <View style={s.waiting}><ActivityIndicator /></View> : null}
        {error ? <Card><Text style={s.empty}>{error}</Text></Card> : null}

        {!loading && !error ? (
          <>
            <Card>
              <View style={s.rowBetween}>
                <Text style={s.h}>{proven ? 'Ownership proven' : 'Not proven yet'}</Text>
                <StatusPill text={proven ? 'On file' : 'Wanted'} tone={proven ? 'green' : 'amber'} />
              </View>
              <Text style={s.note}>
                {proven
                  ? 'The deed, or the power of attorney standing in for it, is on file.'
                  : 'A property is not marketed until the title deed — or the power of attorney the owner gave — is on file.'}
              </Text>
            </Card>

            <SectionTitle>Documents</SectionTitle>
            <Card padded={false} style={{ paddingHorizontal: space(4) }}>
              {deedKinds.map((k, i) => {
                const on = held.includes(k.kind);
                return (
                  <ListRow
                    key={k.kind}
                    title={k.label}
                    subtitle={on ? 'On file' : k.proves ? 'Proves the right to let' : 'Worth holding'}
                    meta={filing === k.kind ? 'Filing…' : undefined}
                    right={<StatusPill
                      text={on ? 'Held' : k.proves ? 'Wanted' : 'Optional'}
                      tone={on ? 'green' : k.proves ? 'amber' : 'neutral'}
                    />}
                    onPress={() => add(k.kind, k.label)}
                    last={i === deedKinds.length - 1}
                  />
                );
              })}
            </Card>

            {documents.length ? (
              <>
                <SectionTitle>What is filed</SectionTitle>
                <Card padded={false} style={{ paddingHorizontal: space(4) }}>
                  {documents.map((d, i) => (
                    <ListRow
                      key={d.id}
                      title={d.filename}
                      subtitle={d.kind.replace(/_/g, ' ')}
                      meta={`${d.uploaded_by} · ${d.created_at.slice(0, 10)}`}
                      last={i === documents.length - 1}
                    />
                  ))}
                </Card>
              </>
            ) : null}

            {advisory.length ? (
              <Card>
                <Text style={s.note}>
                  Still worth holding: {advisory.map((a) => a.replace(/_/g, ' ')).join(', ')}.
                </Text>
              </Card>
            ) : null}
          </>
        ) : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  rowBetween: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  h: { ...font.h3, color: color.inkStrong },
  note: { ...font.small, color: color.inkSoft, lineHeight: 19, marginTop: space(2) },
  waiting: { paddingVertical: space(6), alignItems: 'center' },
  empty: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(5) },
});
