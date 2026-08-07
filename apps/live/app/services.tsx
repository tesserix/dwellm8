import React from 'react';
import { Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import { BackHeader, Button, Card, EmptyState, HouseArt, Screen, color, font, space, useBack } from '@dwellm8/mobile-shared';

/**
 * Book a service — paid-for extras, separate from a maintenance request. The
 * vendor marketplace has no backend yet; anything that is the landlord's
 * responsibility is a maintenance request, which is live.
 */
export default function Services() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');

  return (
    <>
      <BackHeader title="Book a service" subtitle="Vetted vendors, fixed prices" onBack={goBack} />
      <Screen>
        <EmptyState
          art={<HouseArt size={180} />}
          title="Home services are coming soon"
          body="Deep cleaning, pest control, AC service and more — vetted vendors at fixed prices, booked from here."
        />
        <Card>
          <Text style={s.body}>
            A leak, a failed geyser, an unsafe fitting — anything that is the landlord's
            responsibility — is a maintenance request instead, and costs you nothing.
          </Text>
          <Button
            label="Raise a maintenance request"
            tone="secondary"
            onPress={() => router.push('/raise')}
            style={{ marginTop: space(4) }}
          />
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  body: { ...font.body, color: color.inkSoft, lineHeight: 21 },
});
