import React from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import { AppHeader, AvatarButton, Card, EmptyState, HouseArt, color, font, space } from '@dwellm8/mobile-shared';
import { useLiveData } from '../../src/data/source';

/** Messaging has no backend yet. Until it does, this tab is honest about it. */
export default function Chat() {
  const router = useRouter();
  const { tenancy } = useLiveData();

  return (
    <View style={{ flex: 1, backgroundColor: '#F7FAFC' }}>
      <AppHeader
        title="Messages"
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      {tenancy.agency ? (
        <View style={s.sub}><Text style={s.subText}>{tenancy.agency}</Text></View>
      ) : null}

      <EmptyState
        art={<HouseArt size={180} />}
        title="Messaging is coming soon"
        body="You'll talk to your manager right here — repair updates, visit slots and notices, all on the record."
      />
      <Card style={{ marginHorizontal: space(4) }}>
        <Text style={s.body}>
          Until then, everything that matters still lands in the app: requests under Requests,
          dues and receipts under Payments, and notices on the Home tab.
        </Text>
      </Card>
    </View>
  );
}

const s = StyleSheet.create({
  sub: { backgroundColor: '#FFF', paddingBottom: space(3), alignItems: 'center' },
  subText: { ...font.small, color: color.inkSoft },
  body: { ...font.body, color: color.inkSoft, lineHeight: 21 },
});
