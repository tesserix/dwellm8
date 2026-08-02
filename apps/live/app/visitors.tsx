import React from 'react';
import { Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import { BackHeader, Card, EmptyState, HouseArt, Screen, color, font, space } from '@dwellm8/mobile-shared';
import { useLiveData } from '../src/data/source';

/**
 * Visitors and gate passes — the resident's side of the gate. The gate module
 * has no backend yet; this screen says so rather than staging a lobby.
 */
export default function Visitors() {
  const router = useRouter();
  const { tenancy } = useLiveData();

  return (
    <>
      <BackHeader title="Visitors" subtitle={tenancy.unit} onBack={() => router.back()} />
      <Screen>
        <EmptyState
          art={<HouseArt size={180} />}
          title="Gate passes are coming soon"
          body="Pre-approve a guest, a delivery or daily help with a code, so nobody at the gate has to decide on your behalf."
        />
        <Card>
          <Text style={s.body}>
            When this arrives, the gate sees a name and your flat number — never your phone number.
            If a visitor arrives without a code, the guard asks you first.
          </Text>
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  body: { ...font.body, color: color.inkSoft, lineHeight: 21 },
});
