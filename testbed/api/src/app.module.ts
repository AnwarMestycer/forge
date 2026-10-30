import { Module } from '@nestjs/common';
import { JwtModule } from '@nestjs/jwt';
import { AuthController } from './auth/auth.controller';
import { AuthService } from './auth/auth.service';
import { JwtGuard } from './auth/jwt.guard';
import { DbService } from './db.service';
import { HealthController } from './health.controller';
import { UsersController } from './users.controller';

@Module({
  imports: [
    JwtModule.register({
      secret: process.env.JWT_SECRET,
      signOptions: { expiresIn: Number(process.env.JWT_EXPIRES_IN_SECONDS ?? 900) },
    }),
  ],
  controllers: [AuthController, UsersController, HealthController],
  providers: [DbService, AuthService, JwtGuard],
})
export class AppModule {}
