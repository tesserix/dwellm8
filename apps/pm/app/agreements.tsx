import React from 'react';
import { View, Text, StyleSheet, ActivityIndicator, Linking, Alert } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, ErrorState, Screen, ListRow, SectionTitle, StatusPill,
  color, font, space,
} from '@dwellm8/mobile-shared';
import { useDocumentTemplates } from '../src/data/templates';

const titles: Record<string, string> = {
  management_agreement: 'Owner–manager agreement',
  onboarding_checklist: 'Onboarding checklist',
  rent_agreement: 'Rent agreement (11 months)',
  lease_deed: 'Lease deed (registered)',
  power_of_attorney: 'Limited power of attorney',
};

/**
 * The agreements this firm issues (#341). Signing is on paper for now: the
 * manager downloads the .docx, it is signed, and the copy is uploaded back.
 */
export default function AgreementsScreen() {
  const router = useRouter();
  const { loading, error, templates, download, reload } = useDocumentTemplates();

  async function open(id: string, name: string) {
    try {
      const url = await download(id);
      if (!url) {
        Alert.alert(name, 'This template has no file behind it yet.');
        return;
      }
      await Linking.openURL(url);
    } catch (err) {
      Alert.alert(name, (err as Error).message);
    }
  }

  return (
    <>
      <BackHeader
        title="Agreements"
        subtitle="Download, sign on paper, upload the signed copy"
        onBack={() => router.back()}
      />
      <Screen>
        {loading ? <View style={s.waiting}><ActivityIndicator /></View> : null}
        {error ? <ErrorState error={error} onRetry={reload} /> : null}

        {!loading && !error ? (
          <>
            <SectionTitle>What this firm issues</SectionTitle>
            <Card padded={false} style={{ paddingHorizontal: space(4) }}>
              {templates.map((t, i) => (
                <ListRow
                  key={t.id}
                  title={titles[t.kind] ?? t.name}
                  subtitle={t.name}
                  meta={`v${t.version} · ${t.merge_fields.length} fields to fill`}
                  right={
                    <StatusPill
                      text={t.is_default ? 'Standard' : 'Yours'}
                      tone={t.is_default ? 'blue' : 'green'}
                    />
                  }
                  onPress={() => open(t.id, t.name)}
                  last={i === templates.length - 1}
                />
              ))}
              {!templates.length ? (
                <Text style={s.empty}>No templates are published yet.</Text>
              ) : null}
            </Card>

            <Card>
              <Text style={s.note}>
                A manager may enter and manage the property on the owner&apos;s behalf. The
                agreement gives no authority to sell, mortgage or otherwise deal with the
                property, and an owner intending to sell must give four months&apos; notice.
              </Text>
            </Card>
          </>
        ) : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  waiting: { paddingVertical: space(6), alignItems: 'center' },
  empty: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(5) },
  note: { ...font.small, color: color.inkSoft, lineHeight: 19 },
});
