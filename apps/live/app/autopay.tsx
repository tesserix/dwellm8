import React from 'react';
import { Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import { BackHeader, Button, Card, Screen, Timeline, color, font, space } from '@dwellm8/mobile-shared';
import { useLiveData } from '../src/data/source';

/**
 * UPI Autopay. The mandate rails (ADR-0022) exist server-side for
 * manager-initiated collection; the tenant-owned mandate arrives with its own
 * consent flow, and until it does this screen makes no promises.
 */
export default function Autopay() {
  const router = useRouter();
  const { tenancy } = useLiveData();

  return (
    <>
      <BackHeader title="Autopay" subtitle={tenancy.unit} onBack={() => router.back()} />
      <Screen>
        <Card style={{ marginTop: space(3) }}>
          <Text style={s.h}>Never think about rent again</Text>
          <Text style={s.body}>
            A UPI Autopay mandate debits only up to a cap you approve, only on the day you approve,
            and you can pause it at any time. Setting one up from this app is coming soon.
          </Text>
          <Timeline
            items={[
              { at: 'Soon', what: 'Approve a mandate in your UPI app, with a cap you set' },
              { at: 'Each month', what: 'The debit is presented on your chosen day' },
              { at: 'Same day', what: 'Your receipt lands here the moment it confirms', done: false },
            ]}
          />
        </Card>
        <Button
          label="Pay this month instead"
          onPress={() => router.push('/(tabs)/pay')}
          style={{ marginHorizontal: space(4) }}
        />
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21, marginBottom: space(3) },
});
